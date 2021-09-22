package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/youngjae-lim/go-stripe/internal/cards"
	"github.com/youngjae-lim/go-stripe/internal/encryption"
	"github.com/youngjae-lim/go-stripe/internal/models"
	"github.com/youngjae-lim/go-stripe/internal/urlsigner"
)

// Home displays a homepage
func (app *application) Home(w http.ResponseWriter, r *http.Request) {
	if err := app.renderTemplate(w, r, "home", &templateData{}); err != nil {
		app.errorLog.Println(err)
	}
}

// VirtualTerminal displays a virtual terminal to charge credit card
func (app *application) VirtualTerminal(w http.ResponseWriter, r *http.Request) {
	if err := app.renderTemplate(w, r, "terminal", &templateData{}); err != nil {
		app.errorLog.Println(err)
	}
}

type TransanctionData struct {
	FirstName       string
	LastName        string
	Email           string
	PaymentIntentID string
	PaymentMethodID string
	PaymentAmount   int
	PaymentCurrency string
	LastFour        string
	ExpiryMonth     int
	ExpiryYear      int
	BankReturnCode  string
}

// GetTransactionData gets transaction data from post and stripe
func (app *application) GetTransactionData(r *http.Request) (TransanctionData, error) {
	var txnData TransanctionData

	err := r.ParseForm()
	if err != nil {
		app.errorLog.Println(err)
		return txnData, err
	}

	// read posted data
	firstName := r.Form.Get("first_name")
	lastName := r.Form.Get("last_name")
	email := r.Form.Get("cardholder_email")
	paymentID := r.Form.Get("payment_id")
	paymentMethod := r.Form.Get("payment_method")
	paymentAmount := r.Form.Get("payment_amount")
	paymentCurrency := r.Form.Get("payment_currency")
	amount, _ := strconv.Atoi(paymentAmount)

	// create a card with a key and a secret
	card := cards.Card{
		Secret: app.config.stripe.secret,
		Key:    app.config.stripe.key,
	}

	// Get the payment intent by payment intent id
	pi, err := card.RetrievePaymentIntent(paymentID)
	if err != nil {
		app.errorLog.Println(err)
		return txnData, err
	}

	// Get the payment method details by payment method id
	pm, err := card.RetrievePaymentMethod(paymentMethod)
	if err != nil {
		app.errorLog.Println(err)
		return txnData, err
	}

	// extract information from payment intent(pi) and payment method(pm)
	bankReturnCode := pi.Charges.Data[0].ID
	lastFour := pm.Card.Last4
	expiryMonth := pm.Card.ExpMonth
	expiryYear := pm.Card.ExpYear

	txnData = TransanctionData{
		FirstName:       firstName,
		LastName:        lastName,
		Email:           email,
		PaymentIntentID: paymentID,
		PaymentMethodID: paymentMethod,
		PaymentAmount:   amount,
		PaymentCurrency: paymentCurrency,
		LastFour:        lastFour,
		ExpiryMonth:     int(expiryMonth),
		ExpiryYear:      int(expiryYear),
		BankReturnCode:  bankReturnCode,
	}

	return txnData, nil
}

type Invoice struct {
	ID        int       `json:"id"`
	Quantity  int       `json:"quantity"`
	Amount    int       `json:"amount"`
	Product   string    `json:"product"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// PaymentSucceeded displays the confirmation page upon payment
func (app *application) PaymentSucceeded(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// read posted data
	widgetID, _ := strconv.Atoi(r.Form.Get("product_id"))

	// get the transaction data
	txnData, err := app.GetTransactionData(r)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// create a customer
	customer := models.Customer{
		FirstName: txnData.FirstName,
		LastName:  txnData.LastName,
		Email:     txnData.Email,
	}

	// save a customer
	customerID, err := app.SaveCustomer(customer)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// create a new transaction
	txn := models.Transaction{
		Amount:              txnData.PaymentAmount,
		Currency:            txnData.PaymentCurrency,
		LastFour:            txnData.LastFour,
		BankReturnCode:      txnData.BankReturnCode,
		TransactionStatusID: 2,
		ExpiryMonth:         txnData.ExpiryMonth,
		ExpiryYear:          txnData.ExpiryYear,
		PaymentIntent:       txnData.PaymentIntentID,
		PaymentMethod:       txnData.PaymentMethodID,
	}

	// save the transaction
	txnID, err := app.SaveTransaction(txn)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// create a new order
	order := models.Order{
		WidgetID:      widgetID,
		TransactionID: txnID,
		CustomerID:    customerID,
		StatusID:      1,
		Quantity:      1, // TODO: hardcoded as 1 for now
		Amount:        txnData.PaymentAmount,
	}

	// save the order
	orderID, err := app.SaveOrder(order)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// call invoice microservice
	inv := Invoice{
		ID:        orderID,
		Amount:    order.Amount,
		Product:   "Widget",
		Quantity:  order.Quantity,
		FirstName: txnData.FirstName,
		LastName:  txnData.LastName,
		Email:     txnData.Email,
		CreatedAt: time.Now(),
	}

	err = app.callInvoiceMicro(inv)
	if err != nil {
		app.errorLog.Println(err)
	}


	// write transaction data to session, and then redirect the user to new page
	// to avoid posting data twice
	app.Session.Put(r.Context(), "receipt", txnData)
	http.Redirect(w, r, "/receipt", http.StatusSeeOther)
}

// callInvoiceMicro makes a POST request to invoice microservice
func (app *application) callInvoiceMicro(inv Invoice) error {
	url := "http://localhost:5000/invoice/create-and-send"
	out, err := json.MarshalIndent(inv, "", "\t")
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(out))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	app.infoLog.Println(resp.Body)

	return nil
}

// VirtualTerminalPaymentSucceeded displays the confirmation page for virtual terminal transaction
// ! The functionality of VirtualTerminalPaymentSucceeded handler is moved into the backend
func (app *application) VirtualTerminalPaymentSucceeded(w http.ResponseWriter, r *http.Request) {
	// get the transaction data
	txnData, err := app.GetTransactionData(r)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// create a new transaction
	txn := models.Transaction{
		Amount:              txnData.PaymentAmount,
		Currency:            txnData.PaymentCurrency,
		LastFour:            txnData.LastFour,
		BankReturnCode:      txnData.BankReturnCode,
		TransactionStatusID: 2,
		ExpiryMonth:         txnData.ExpiryMonth,
		ExpiryYear:          txnData.ExpiryYear,
		PaymentIntent:       txnData.PaymentIntentID,
		PaymentMethod:       txnData.PaymentMethodID,
	}

	// save the transaction
	_, err = app.SaveTransaction(txn)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// write transaction data to session, and then redirect the user to new page
	// to avoid posting data twice
	app.Session.Put(r.Context(), "receipt", txnData)
	http.Redirect(w, r, "/virtual-terminal-receipt", http.StatusSeeOther)
}

func (app *application) Receipt(w http.ResponseWriter, r *http.Request) {
	// retrieve receipt data from session
	txn, ok := app.Session.Get(r.Context(), "receipt").(TransanctionData)
	if !ok {
		app.errorLog.Println("can't get data from session")
		// redirect to homepage, for example, when the receipt page is refreshed
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
	data := make(map[string]interface{})
	data["txn"] = txn

	// once retrieved, remove receipt data from session
	app.Session.Remove(r.Context(), "receipt")

	// render receipt.page.gohtml
	if err := app.renderTemplate(w, r, "receipt", &templateData{Data: data}); err != nil {
		app.errorLog.Println(err)
	}
}

// ! The functionality of VirtualTerminalReceipt handler is moved into the backend
func (app *application) VirtualTerminalReceipt(w http.ResponseWriter, r *http.Request) {
	// retrieve receipt data from session
	txn, ok := app.Session.Get(r.Context(), "receipt").(TransanctionData)
	if !ok {
		app.errorLog.Println("can't get data from session")
		// redirect to homepage, for example, when the receipt page is refreshed
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
	data := make(map[string]interface{})
	data["txn"] = txn

	// once retrieved, remove receipt data from session
	app.Session.Remove(r.Context(), "receipt")

	// render receipt.page.gohtml
	if err := app.renderTemplate(w, r, "virtual-terminal-receipt", &templateData{Data: data}); err != nil {
		app.errorLog.Println(err)
	}
}

// SaveCustomer saves a customer and returns its id
func (app *application) SaveCustomer(customer models.Customer) (int, error) {
	id, err := app.DB.InsertCustomer(customer)
	if err != nil {
		app.errorLog.Println(err)
		return 0, err
	}

	return id, nil
}

// SaveTransaction saves a transaction and returns its id
func (app *application) SaveTransaction(txn models.Transaction) (int, error) {
	id, err := app.DB.InsertTransaction(txn)
	if err != nil {
		app.errorLog.Println(err)
		return 0, err
	}

	return id, nil
}

// SaveOrder saves an order and returns its id
func (app *application) SaveOrder(order models.Order) (int, error) {
	id, err := app.DB.InsertOrder(order)
	if err != nil {
		app.errorLog.Println(err)
		return 0, err
	}

	return id, nil
}

// ChargeOnce displays the page to buy one widget
func (app *application) ChargeOnce(w http.ResponseWriter, r *http.Request) {
	// extract id from url params
	// example: localhost:4000/widget/{id}
	id := chi.URLParam(r, "id")
	widgetID, _ := strconv.Atoi(id)

	// get a widget by id
	widget, err := app.DB.GetWidget(widgetID)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// make a map of widget data to be passed onto the buy-once.page.gohtml
	data := make(map[string]interface{})
	data["widget"] = widget

	// render the template
	if err := app.renderTemplate(w, r, "buy-once", &templateData{Data: data}, "stripe-js"); err != nil {
		app.errorLog.Println(err)
	}
}

func (app *application) BronzePlan(w http.ResponseWriter, r *http.Request) {
	// Get a monthly planed widget
	// id = 2 is hardcoded for now due to only one subscription-based plan available
	widget, err := app.DB.GetWidget(2)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	data := make(map[string]interface{})
	data["widget"] = widget

	// render the template with widget data pulled from database
	if err := app.renderTemplate(w, r, "bronze-plan", &templateData{Data: data}); err != nil {
		app.errorLog.Println(err)
	}
}

func (app *application) BronzePlanReceipt(w http.ResponseWriter, r *http.Request) {
	// render the template
	if err := app.renderTemplate(w, r, "subscription-receipt", &templateData{}); err != nil {
		app.errorLog.Println(err)
	}
}

func (app *application) LoginPage(w http.ResponseWriter, r *http.Request) {
	// render the template
	if err := app.renderTemplate(w, r, "login", &templateData{}); err != nil {
		app.errorLog.Println(err)
	}
}

func (app *application) PostLoginPage(w http.ResponseWriter, r *http.Request) {
	app.Session.RenewToken(r.Context())

	err := r.ParseForm()
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	email := r.Form.Get("email")
	password := r.Form.Get("password")

	id, err := app.DB.Authenticate(email, password)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	app.Session.Put(r.Context(), "userID", id)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Logout destroys the exising session data and renews the session token, then redirects a user to a login page
func (app *application) Logout(w http.ResponseWriter, r *http.Request) {
	app.Session.Destroy(r.Context())
	app.Session.RenewToken(r.Context())

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (app *application) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	// render the template
	if err := app.renderTemplate(w, r, "forgot-password", &templateData{}); err != nil {
		app.errorLog.Println(err)
	}
}

func (app *application) ShowResetPassword(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	theURL := r.RequestURI
	testURL := fmt.Sprintf("%s%s", app.config.frontend_url, theURL)

	signer := urlsigner.Signer{
		Secret: []byte(app.config.pwreset_secretkey),
	}

	// verify the signed url with a token
	valid := signer.VerifyToken(testURL)
	if !valid {
		app.errorLog.Println("Invalid url - tampering detected")
		return
	}

	// check if the password reset link has not expired yet
	expired := signer.IsExpired(testURL, 60)
	if expired {
		app.errorLog.Println("Password Reset Link Expired")
		return
	}

	// encrypt the email
	encryptor := encryption.Encryption{
		Key: []byte(app.config.pwreset_secretkey),
	}

	encryptedEmail, err := encryptor.Encrypt(email)
	if err != nil {
		app.errorLog.Printf("Encryption failed")
		return
	}

	data := make(map[string]interface{})
	data["email"] = encryptedEmail

	// render the template
	if err := app.renderTemplate(w, r, "reset-password", &templateData{Data: data}); err != nil {
		app.errorLog.Println(err)
	}
}

func (app *application) AllSales(w http.ResponseWriter, r *http.Request) {
	// render the template
	if err := app.renderTemplate(w, r, "all-sales", &templateData{}); err != nil {
		app.errorLog.Println(err)
	}
}

func (app *application) AllSubscriptions(w http.ResponseWriter, r *http.Request) {
	// render the template
	if err := app.renderTemplate(w, r, "all-subscriptions", &templateData{}); err != nil {
		app.errorLog.Println(err)
	}
}

func (app *application) ShowSale(w http.ResponseWriter, r *http.Request) {
	stringMap := make(map[string]string)
	stringMap["title"] = "Sale"
	stringMap["cancel"] = "/admin/all-sales"
	stringMap["request-url"] = "/api/admin/refund"
	stringMap["refund-cancel-btn"] = "Refund Order"
	stringMap["success-msg"] = "Charge refunded"
	stringMap["charged-or-subscribed"] = "Charged"
	stringMap["refunded-or-cancelled"] = "Refunded"

	// render the template
	if err := app.renderTemplate(w, r, "sale", &templateData{StringMap: stringMap}); err != nil {
		app.errorLog.Println(err)
	}
}

func (app *application) ShowSubscription(w http.ResponseWriter, r *http.Request) {
	stringMap := make(map[string]string)
	stringMap["title"] = "Subscription"
	stringMap["cancel"] = "/admin/all-subscriptions"
	stringMap["request-url"] = "/api/admin/cancel-subscription"
	stringMap["refund-cancel-btn"] = "Cancel Subscription"
	stringMap["success-msg"] = "Subscription cancelled"
	stringMap["charged-or-subscribed"] = "Subscribed"
	stringMap["refunded-or-cancelled"] = "Cancelled"

	// render the template
	if err := app.renderTemplate(w, r, "sale", &templateData{StringMap: stringMap}); err != nil {
		app.errorLog.Println(err)
	}
}

// AllUsers shows all admin users
func (app *application) AllUsers(w http.ResponseWriter, r *http.Request) {

	// render the template
	if err := app.renderTemplate(w, r, "all-users", &templateData{}); err != nil {
		app.errorLog.Println(err)
	}
}

// OneUser shows one admin user
func (app *application) OneUser(w http.ResponseWriter, r *http.Request) {

	// render the template
	if err := app.renderTemplate(w, r, "one-user", &templateData{}); err != nil {
		app.errorLog.Println(err)
	}
}
