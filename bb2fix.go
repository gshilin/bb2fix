// POST
// Update payment status
// CGO_ENABLED=0 go build bb2fix.go ; strip bb2fix; cp bb2fix /media/sf_D_DRIVE/projects/bbpriority/

// http://dev2.org.kbb1.com/sites/all/modules/civicrm/extern/rest.php?entity=Contribution&action=create&api_key=userkey&key=sitekey&json={"debug":1,"sequential":1,"financial_type_id":"כנס גני התערוכה","total_amount":1740,"contact_id":83916,"id":51409,"contribution_status_id":"Completed"}

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	_ "github.com/jmoiron/sqlx"
	_ "github.com/joho/godotenv/autoload"
	_ "github.com/pkg/errors"
)

// Read messages from database
type databaseType struct {
	DB *sqlx.DB
}

type pelecardType struct {
	user     string
	password string
	terminal string
}

type resultData struct {
	CreatedDate                  string
	BroadcastDate                string
	BroadcastNo                  string
	PelecardTransactionId        string
	VoucherId                    string
	ShvaResult                   string
	ShvaFileNumber               string
	StationNumber                string
	Reciept                      string
	JParam                       string
	CreditCardNumber             string
	CreditCardExpDate            string
	CreditCardCompanyClearer     string
	CreditCardCompanyIssuer      string
	CreditCardStarsDiscountTotal string
	CreditType                   string
	CreditCardAbroadCard         string
	DebitType                    string
	DebitCode                    string
	DebitTotal                   string
	DebitApproveNumber           string
	DebitCurrency                string
	TotalPayments                string
	FirstPaymentTotal            string
	FixedPaymentTotal            string
	AdditionalDetailsParamX      string
}

type pelecardResponse struct {
	StatusCode   string
	ErrorMessage string
	ResultData   resultData
}

var (
	database  databaseType
	pelecards [4]pelecardType
	err       error
)

func main() {

	file, err := os.OpenFile("bb2fix.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	log.SetOutput(file)

	host := os.Getenv("CIVI_HOST")
	if host == "" {
		host = "localhost"
	}
	dbName := os.Getenv("CIVI_DBNAME")
	if dbName == "" {
		dbName = "localhost"
	}
	user := os.Getenv("CIVI_USER")
	if user == "" {
		log.Fatalf("Unable to connect without username\n")
	}
	password := os.Getenv("CIVI_PASSWORD")
	if password == "" {
		log.Fatalf("Unable to connect without password\n")
	}
	protocol := os.Getenv("CIVI_PROTOCOL")
	if protocol == "" {
		log.Fatalf("Unable to connect without protocol\n")
	}
	startFromS := os.Getenv("CIVI_START_FROM")
	var startFrom int
	if startFromS == "" {
		startFrom = 51247
	} else {
		if startFrom, err = strconv.Atoi(startFromS); err != nil {
			log.Fatalf("Wrong value for Start From: (%s) %s\n", startFromS, err)
		}
	}

	database.DB = OpenDb(host, user, password, protocol, dbName)
	defer database.closeDb()

	pelecards = OpenPelecard()

	ReadMessagesToFix(startFrom)
	UpdateCustomFields()
}

func OpenPelecard() (p [4]pelecardType) {
	p[0].user = os.Getenv("PELECARD_USER")
	p[0].password = os.Getenv("PELECARD_PASSWORD")
	p[0].terminal = os.Getenv("ben2_PELECARD_TERMINAL")
	if p[0].user == "" || p[0].password == "" || p[0].terminal == "" {
		log.Fatalf("PELECARD 1 parameters are missing")
	}
	p[1].user = os.Getenv("PELECARD_USER1")
	p[1].password = os.Getenv("PELECARD_PASSWORD1")
	p[1].terminal = os.Getenv("PELECARD_RECURR_TERMINAL")
	if p[1].user == "" || p[1].password == "" || p[1].terminal == "" {
		log.Fatalf("PELECARD 2 parameters are missing")
	}
	p[2].user = os.Getenv("meshp18_PELECARD_USER")
	p[2].password = os.Getenv("meshp18_PELECARD_PASSWORD")
	p[2].terminal = os.Getenv("meshp18_PELECARD_TERMINAL")
	if p[2].user == "" || p[2].password == "" || p[2].terminal == "" {
		log.Fatalf("PELECARD 2 parameters are missing")
	}
	p[3].user = os.Getenv("PELECARD_USER1")
	p[3].password = os.Getenv("PELECARD_PASSWORD1")
	p[3].terminal = os.Getenv("PELECARD_TERMINAL1")
	if p[3].user == "" || p[3].password == "" || p[3].terminal == "" {
		log.Fatalf("PELECARD 3 parameters are missing")
	}
	return
}

// Connect to DB
func OpenDb(host string, user string, password string, protocol string, dbName string) (db *sqlx.DB) {
	dsn := fmt.Sprintf("%s:%s@%s(%s)/%s", user, password, protocol, host, dbName)
	if db, err = sqlx.Open("mysql", dsn); err != nil {
		log.Fatalf("DB connection error: %v\n", err)
	}
	if err = db.Ping(); err != nil {
		log.Fatalf("DB real connection error: %v\n", err)
	}
	return
}

func (db databaseType) closeDb() {
	_ = db.DB.Close()
}

// Update custom fields
func UpdateCustomFields() {
	type group struct {
		ContributionId string `db:"contribution_id"`
		ContactId      string `db:"contact_id"`
		Token          string `db:"token"`
		Cardnum        string `db:"cardnum"`
		Cardexp        string `db:"cardexp"`
		Reference      string `db:"reference"`
	}
	groups := make([]group, 0)

	log.Println("UpdateCustomFields -- Fetching group212...")
	// find all contributions that are completed, but don't have custom fields
	err = database.DB.Select(&groups, `
		SELECT DISTINCT p.cid contribution_id, cc.contact_id, COALESCE(p.token, "") token, p.cardnum cardnum, p.cardexp cardexp, CONCAT('civicrm-', p.cid) AS reference
		FROM civicrm_bb_payment_responses p
		INNER JOIN civicrm_contribution cc ON p.cid = cc.id	AND cc.invoice_number = 1
		LEFT OUTER JOIN civicrm_value_payment_detai_212 d ON d.entity_id = p.cid
		WHERE d.id IS NULL
`)
	if err != nil {
		log.Printf("UpdateCustomFields -- Fetch error %v\n", err)
		fck(err)
	}
	log.Printf("UpdateCustomFields -- Fetched %d records\n", len(groups))
	if len(groups) > 0 {
		log.Println("UpdateCustomFields -- Updating group212...")
		for _, group := range groups {
			_, err = database.DB.Exec(`
			INSERT INTO civicrm_value_payment_detai_212(entity_id, token_1187, cardnum_1188, cardexp_1189, reference_1190)
			VALUES (?, ?, ?, ?, ?)
		`, group.ContributionId, group.Token, group.Cardnum, group.Cardexp, group.Reference)
			if err != nil {
				log.Printf("UpdateCustomFields212 -- Insert error %v\n", err)
				fck(err)
			}
			if group.Token != "" {
				log.Println("UpdateCustomFields -- Updating group213...")
				_, err = database.DB.Exec(`
			INSERT INTO civicrm_value_general_token_213(entity_id, gtoken_1191)
			VALUES (?, ?)
			ON DUPLICATE KEY UPDATE gtoken_1191 = ?
		`, group.ContactId, group.Token, group.Token)
				if err != nil {
					log.Printf("UpdateCustomFields213 -- Insert error %v\n", err)
					fck(err)
				}
			}
		}
		log.Println("UpdateCustomFields -- Update completed...")
	}
}

func ReadMessagesToFix(startFrom int) {

	completed := database.getStatus("Completed") // 1
	pending := database.getStatus("Pending")     // 2
	cancelled := database.getStatus("Cancelled") // 3

	log.Println("START run: CHECK INCOMPLETED TRANSACTIONS ---------------------------------")
	contributionIds := database.getContributionIdsIncompleted(pending, startFrom)
	log.Println("Found", len(contributionIds), "contributions")

	for _, id := range contributionIds {
		// Check if bb_payment_responses present
		log.Println(id, "-- testing")
		if database.paymentDataExists(id) {
			log.Println(id, "-- payment exists, marking as completed")
			// If yes - mark contribution as done and finish
			database.fixContribution(id, completed)
			continue
		}
		// If no - request PeleCard for this transaction using ParamX
		var response pelecardResponse
		var wasError = false
		for _, pelecard := range pelecards {
			log.Println(id, "-- payment not exists, requesting PeleCard", pelecard.user)
			response, err = pelecard.getPelecardTransaction(id)
			if err == nil {
				if response.StatusCode == "000" && response.ResultData.ShvaResult != "000" {
					log.Println(id, "-- response PeleCard. Removed transaction, ShvaResult:", response.ResultData.ShvaResult)
					break
				}
				if response.StatusCode == "000" && response.ResultData.ShvaResult == "000" {
					log.Println(id, "-- response PeleCard. Transaction found")
					break
				}
				log.Println(id, "-- response PeleCard. No error, but status", response.StatusCode, "transaction id", response.ResultData.PelecardTransactionId)
				wasError = true
				continue
			}
			if wasError {
				log.Println(id, "-- second error", pelecard.user)
				fck(err)
			} else {
				log.Println(id, "-- first error", pelecard.user)
				wasError = true
			}
		}
		log.Println(id, "-- response PeleCard", "status", response.StatusCode, "transaction id", response.ResultData.PelecardTransactionId)
		if response.StatusCode != "000" || response.ResultData.ShvaResult != "000" {
			// If there was no payment - mark contribution as cancelled
			log.Println(id, "-- no payment found on PeleCard; marking as cancelled")
			database.fixContribution(id, cancelled)
		} else {
			// If information on PeleCard present -- insert bb_payment_response record and mark contribution as done
			log.Println(id, "-- payment found on PeleCard !!!; marking as completed")
			database.insertPayment(id, response)
			database.fixContribution(id, completed)
		}
	}
	log.Println("FINISH run: CHECK INCOMPLETED TRANSACTIONS ---------------------------------")

	log.Println("START run: CHECK COMPLETED TRANSACTIONS WITH ERROR ------------------------")
	contributionIds = database.getContributionIdsCompleted(completed, startFrom)
	log.Println("Found", len(contributionIds), "contributions")

	for _, id := range contributionIds {
		// Check if bb_payment_responses present
		log.Println(id, "-- testing")
		if database.paymentDataExists(id) {
			log.Println(id, "-- payment exists, marking as completed")
			// If yes - mark contribution as done and finish
			database.fixInvoiceNumber(id, "")
			continue
		}
		// If no - request PeleCard for this transaction using ParamX
		log.Println(id, "-- payment not exists, requesting PeleCard(s)")
		var response pelecardResponse
		var wasError = false
		for _, pelecard := range pelecards {
			response, err = pelecard.getPelecardTransaction(id)
			if err == nil {
				if response.StatusCode == "000" && response.ResultData.ShvaResult != "000" {
					log.Println(id, "-- response PeleCard. Removed transaction, ShvaResult:", response.ResultData.ShvaResult)
					break
				}
				if response.StatusCode == "000" && response.ResultData.ShvaResult == "000" {
					log.Println(id, "-- response PeleCard. Transaction found")
					break
				}
				log.Println(id, "-- response PeleCard. No error, but status", response.StatusCode, "transaction id", response.ResultData.PelecardTransactionId)
				wasError = true
				continue
			}
			if wasError {
				log.Println(id, "-- second error", pelecard.user)
				fck(err)
			} else {
				log.Println(id, "-- first error", pelecard.user)
				wasError = true
			}
		}
		log.Println(id, "-- response PeleCard", "status", response.StatusCode, "transaction id", response.ResultData.PelecardTransactionId)
		if response.StatusCode != "000" || response.ResultData.ShvaResult != "000" {
			// If there was no payment - mark contribution
			log.Println(id, "-- no payment found on PeleCard; marking")
			newCode := fmt.Sprintf("-%s", response.StatusCode)
			database.fixInvoiceNumber(id, newCode)
		} else {
			// If information on PeleCard present -- insert bb_payment_response record and mark contribution as done
			log.Println(id, "-- payment found on PeleCard !!!; marking as completed")
			database.insertPayment(id, response)
			database.fixInvoiceNumber(id, "")
		}
	}
	log.Println("FINISH run: CHECK COMPLETED TRANSACTIONS WITH ERROR ------------------------")
}

func (db databaseType) insertPayment(id int64, response pelecardResponse) {
	var firstpay float64
	var debitTotal float64

	debitTotal, _ = strconv.ParseFloat(response.ResultData.DebitTotal, 64)
	debitTotal = debitTotal / 100
	if response.ResultData.TotalPayments == "1" {
		firstpay = debitTotal
	} else {
		firstpay, _ = strconv.ParseFloat(response.ResultData.FirstPaymentTotal, 64)
		firstpay = firstpay / 100
	}

	db.DB.MustExec(`
		INSERT INTO civicrm_bb_payment_responses(trxn_id, cid, cardtype, cardnum, cardexp, firstpay, installments, response, amount, created_at)
                   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?,  NOW())
	`,
		response.ResultData.PelecardTransactionId,
		id,
		response.ResultData.CreditCardCompanyIssuer,
		response.ResultData.CreditCardNumber,
		response.ResultData.CreditCardExpDate,
		firstpay,
		response.ResultData.TotalPayments,
		fmt.Sprintf("%#v", response.ResultData),
		debitTotal,
	)

	return
}

func (p pelecardType) getPelecardTransaction(id int64) (response pelecardResponse, err error) {
	type pelecardRequest struct {
		TerminalNumber  string `json:"terminalNumber"`
		User            string `json:"user"`
		Password        string `json:"password"`
		ShopNumber      string `json:"shopNumber"`
		ParamX          string `json:"paramX"`
		ShvaSuccessOnly string `json:"shvaSuccessOnly"`
	}

	var request = pelecardRequest{
		p.terminal,
		p.user,
		p.password,
		"1000",
		fmt.Sprintf("civicrm-%d", id),
		"true",
	}
	params, _ := json.Marshal(request)
	resp, err := http.Post("https://gateway20.pelecard.biz/services/CheckGoodParamX", "application/json", bytes.NewBuffer(params))
	if err != nil {
		return
	}
	if resp.StatusCode != 200 {
		return
	}

	bodyBytes, _ := ioutil.ReadAll(resp.Body)
	_ = json.Unmarshal(bodyBytes, &response)
	resp.Body.Close()

	return
}

func (db databaseType) fixContribution(id int64, status int64) {
	db.DB.MustExec(`UPDATE civicrm_contribution SET contribution_status_id = ? WHERE id = ?`, status, id)
}

func (db databaseType) fixInvoiceNumber(id int64, state string) {
	if state == "" {
		db.DB.MustExec(`UPDATE civicrm_contribution SET invoice_number = NULL WHERE id = ?`, id)
	} else {
		db.DB.MustExec(`UPDATE civicrm_contribution SET invoice_number = ? WHERE id = ?`, state, id)
	}
}

func (db databaseType) paymentDataExists(id int64) (x bool) {
	err = db.DB.Get(&x, `
		SELECT count(1) > 0
		FROM civicrm_bb_payment_responses
		WHERE cid = ?
	`, id)
	fck(err)
	return
}

func (db databaseType) getContributionIdsIncompleted(status int64, startFromId int) (ids []int64) {
	loc, _ := time.LoadLocation("Asia/Jerusalem")
	d := time.Duration(3 * time.Hour)
	t := time.Now().In(loc).Add(-d)

	err = db.DB.Select(&ids, `
		SELECT id
		FROM civicrm_contribution
		WHERE contribution_status_id = ? AND id >= ? AND receive_date < ? AND invoice_number IS NULL
	`, status, startFromId, t.Format("2006-01-02 15:04:05"))
	fck(err)
	return
}

func (db databaseType) getContributionIdsCompleted(status int64, startFromId int) (ids []int64) {
	loc, _ := time.LoadLocation("Asia/Jerusalem")
	d := time.Duration(30 * time.Hour * 24)
	t := time.Now().In(loc).Add(-d)

	err = db.DB.Select(&ids, `
		SELECT id
		FROM civicrm_contribution
		WHERE contribution_status_id = ? AND id >= ? AND receive_date < ? AND (invoice_number IS NOT NULL AND invoice_number != '1')
	`, status, startFromId, t.Format("2006-01-02 15:04:05"))
	fck(err)
	return
}

func (db databaseType) getStatus(statusName string) (status int64) {
	err = db.DB.Get(&status, `
		SELECT value
		FROM civicrm_option_value
		WHERE option_group_id = (
		  SELECT id contributionStatusID
		  FROM civicrm_option_group
		  WHERE name = "contribution_status"
		  LIMIT 1
		) AND name = ?
	`, statusName)
	return
}

//func updateReported2prio(stmt *sql.Stmt, id string) {
//	res, err := stmt.Exec(id)
//	if err != nil {
//		log.Fatalf("Update error: %v\n", err)
//	}
//	rowCnt, err := res.RowsAffected()
//	if err != nil {
//		log.Fatalf("Update error: %v\n", err)
//	}
//	if rowCnt != 1 {
//		log.Fatalf("Update error: %d rows were updated instead of 1\n", rowCnt)
//	}
//}

func fck(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
