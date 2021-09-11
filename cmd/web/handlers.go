package main

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/youngjae-lim/go-stripe/internal/cards"
	"github.com/youngjae-lim/go-stripe/internal/models"
)

// Home displays a homepage
func (app *application) Home(w http.ResponseWriter, r *http.Request) {
	if err := app.renderTemplate(w, r, "home", &templateData{}); err != nil {
		app.errorLog.Println(err)
	}
}

// VirtualTerminal displays a virtual terminal to charge credit card
func (app *application) VirtualTerminal(w http.ResponseWriter, r *http.Request) {
	if err := app.renderTemplate(w, r, "terminal", &templateData{}, "stripe-js"); err != nil {
		app.errorLog.Println(err)
	}
}

// PaymentSucceeded displays the confirmation page upon payment
func (app *application) PaymentSucceeded(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// read posted data
	firstName := r.Form.Get("first_name")
	lastName := r.Form.Get("last_name")
	email := r.Form.Get("cardholder_email")
	paymentID := r.Form.Get("payment_id")
	paymentMethod := r.Form.Get("payment_method")
	paymentAmount := r.Form.Get("payment_amount")
	paymentCurrency := r.Form.Get("payment_currency")
	widgetID, _ := strconv.Atoi(r.Form.Get("product_id"))

	// create a card with a key and a secret
	card := cards.Card{
		Secret: app.config.stripe.secret,
		Key:    app.config.stripe.key,
	}

	// Get the payment intent by payment id
	pi, err := card.RetrievePaymentIntent(paymentID)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// Get the payment method details by payment id
	pm, err := card.RetrievePaymentMethod(paymentMethod)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// extract information from payment intent(pi) and payment method(pm)
	bankReturnCode := pi.Charges.Data[0].ID
	lastFour := pm.Card.Last4
	expiryMonth := pm.Card.ExpMonth
	expiryYear := pm.Card.ExpYear

	// create a customer
	customer := models.Customer{
		FirstName: firstName,
		LastName:  lastName,
		Email:     email,
	}

	// save a customer
	customerID, err := app.SaveCustomer(customer)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// create a new transaction
	amount, _ := strconv.Atoi(paymentAmount)
	txn := models.Transaction{
		Amount:              amount,
		Currency:            paymentCurrency,
		LastFour:            lastFour,
		BankReturnCode:      bankReturnCode,
		TransactionStatusID: 2,
		ExpiryMonth:         int(expiryMonth),
		ExpiryYear:          int(expiryYear),
		PaymentIntent:       paymentID,
		PaymentMethod:       paymentMethod,
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
		Amount:        amount,
	}

	// save the order
	_, err = app.SaveOrder(order)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	// make a map of data to be passed onto the template page
	data := make(map[string]interface{})
	data["first_name"] = firstName
	data["last_name"] = lastName
	data["email"] = email
	data["payment_intent"] = paymentID
	data["payment_method"] = paymentMethod
	data["payment_amount"] = paymentAmount
	data["payment_currency"] = paymentCurrency
	data["last_four"] = lastFour
	data["expiry_month"] = expiryMonth
	data["expiry_year"] = expiryYear
	data["bank_return_code"] = bankReturnCode

	// should write this data to session, and then redirect the user to new page

	// render succeeded.page.gohtml
	if err := app.renderTemplate(w, r, "succeeded", &templateData{Data: data}); err != nil {
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
