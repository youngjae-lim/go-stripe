package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stripe/stripe-go/v72"
	"github.com/youngjae-lim/go-stripe/internal/cards"
	"github.com/youngjae-lim/go-stripe/internal/encryption"
	"github.com/youngjae-lim/go-stripe/internal/models"
	"github.com/youngjae-lim/go-stripe/internal/urlsigner"
	"golang.org/x/crypto/bcrypt"
)

type stripePayload struct {
	Currency      string `json:"currency"`
	Amount        string `json:"amount"`
	PaymentMethod string `json:"payment_method"`
	Email         string `json:"email"`
	CardBrand     string `json:"card_brand"`
	ExpiryMonth   int    `json:"expiry_month"`
	ExpiryYear    int    `json:"expiry_year"`
	LastFour      string `json:"last_four"`
	PriceID       string `json:"price_id"`
	ProductID     string `json:"product_id"`
	FirstName     string `json:"first_name"`
	LastName      string `json:"last_name"`
}

type jsonResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Content string `json:"content,omitempty"`
	ID      int    `json:"id,omitempty"`
}

func (app *application) GetPaymentIntent(w http.ResponseWriter, r *http.Request) {
	var payload stripePayload

	err := json.NewDecoder(r.Body).Decode(&payload)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	amount, err := strconv.Atoi(payload.Amount)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	card := cards.Card{
		Secret:   app.config.stripe.secret,
		Key:      app.config.stripe.key,
		Currency: payload.Currency,
	}

	okay := true

	pi, msg, err := card.Charge(payload.Currency, amount)
	if err != nil {
		okay = false
	}

	if okay {
		out, err := json.MarshalIndent(pi, "", "    ")
		if err != nil {
			app.errorLog.Println(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	} else {
		j := jsonResponse{
			OK:      false,
			Message: msg,
			Content: "",
		}

		out, err := json.MarshalIndent(j, "", "    ")
		if err != nil {
			app.errorLog.Println(err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	}
}

func (app *application) GetWidgetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	widgetID, _ := strconv.Atoi(id)

	widget, err := app.DB.GetWidget(widgetID)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	out, err := json.MarshalIndent(widget, "", "    ")
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

// SubscribeToPlan handles creating a new customer and subscribing to plan
func (app *application) SubscribeToPlan(w http.ResponseWriter, r *http.Request) {
	var payload stripePayload

	// decode payload:
	err := json.NewDecoder(r.Body).Decode(&payload)
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	app.infoLog.Println("payload is", payload)

	card := cards.Card{
		Secret:   app.config.stripe.secret,
		Key:      app.config.stripe.key,
		Currency: payload.Currency,
	}

	okay := true
	var subscription *stripe.Subscription
	txnMsg := "Transaction successful"

	// create a new stripe customer
	stripeCustomer, msg, err := card.CreateCustomer(payload.PaymentMethod, payload.Email)
	if err != nil {
		app.errorLog.Println(err)
		okay = false
		txnMsg = msg
	}

	// execute SubscribeToPlan only if creating a customer is successful
	if okay {
		// subscribe the customer to plan and get its id
		subscription, err = card.SubscribeToPlan(stripeCustomer, payload.PriceID, payload.Email, payload.LastFour, "")
		if err != nil {
			app.errorLog.Println(err)
			okay = false
			txnMsg = "Error subscribing a new customer"
		}

		app.infoLog.Println("subscription id is", subscription.ID)
	}

	// if both creating a new customer and subscribing went throuhg,
	// we will save the customer, transaction, and order to the database
	if okay {
		// get a widget id
		productID, _ := strconv.Atoi(payload.ProductID)

		// create a new customer
		customer := models.Customer{
			FirstName: payload.FirstName,
			LastName:  payload.LastName,
			Email:     payload.Email,
		}

		// save the customer
		customerID, err := app.SaveCustomer(customer)
		if err != nil {
			app.errorLog.Println(err)
			return
		}

		// create a new transaction
		amount, _ := strconv.Atoi(payload.Amount)

		txn := models.Transaction{
			Amount:              amount,
			Currency:            "usd",
			LastFour:            payload.LastFour,
			TransactionStatusID: 2,
			ExpiryMonth:         payload.ExpiryMonth,
			ExpiryYear:          payload.ExpiryYear,
			PaymentIntent:       subscription.ID,
			PaymentMethod:       payload.PaymentMethod,
		}

		// save the transaction
		txnID, err := app.SaveTransaction(txn)
		if err != nil {
			app.errorLog.Println(err)
			return
		}

		// create a new order
		order := models.Order{
			WidgetID:      productID,
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
	}

	resp := jsonResponse{
		OK:      okay,
		Message: txnMsg,
	}

	out, err := json.MarshalIndent(resp, "", "    ")
	if err != nil {
		app.errorLog.Println(err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
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

// CreateAuthToken receives an email and a password from the frontend login form and verifies them. Once verified, a token will be generated and saved to the tokens table in the database and sent it back to the user
func (app *application) CreateAuthToken(w http.ResponseWriter, r *http.Request) {
	var userInput struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	err := app.readJSON(w, r, &userInput)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	// get the user from database by email; send an error if the email is not valid
	user, err := app.DB.GetUserByEmail(userInput.Email)
	if err != nil {
		app.invalidCredentials(w)
		return
	}

	// validate the password
	// send an error if the password is not valid
	IsPasswordValid, err := app.passwordMatches(user.Password, userInput.Password)
	if err != nil {
		app.invalidCredentials(w)
		return
	}

	if !IsPasswordValid {
		app.invalidCredentials(w)
		return
	}

	// generate a token
	token, err := models.GenerateToken(user.ID, 24*time.Hour, models.ScopeAuthentication)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	// save the hashed token to database
	err = app.DB.InsertToken(token, user)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	// send a response
	var payload struct {
		Error   bool          `json:"error"`
		Message string        `json:"message"`
		Token   *models.Token `json:"authentication_token"`
	}
	payload.Error = false
	payload.Message = fmt.Sprintf("Token for %s created", userInput.Email)
	payload.Token = token

	_ = app.writeJSON(w, http.StatusOK, payload)
}

// authenticateToken validates a received token from a client and returns a associated user with the token.
func (app *application) authenticateToken(r *http.Request) (*models.User, error) {
	// retrieve a token from the client's http request header
	authorizationHeader := r.Header.Get("Authorization")
	if authorizationHeader == "" {
		return nil, errors.New("no authorization header received")
	}

	headerParts := strings.Split(authorizationHeader, " ")
	if len(headerParts) != 2 || headerParts[0] != "Bearer" {
		return nil, errors.New("no authorization header received")
	}

	token := headerParts[1]
	if len(token) != 26 {
		return nil, errors.New("authentication token wrong size")
	}

	// once all passed, get the user from the tokens table in the database
	user, err := app.DB.GetUserForToken(token)
	if err != nil {
		return nil, errors.New("no matching user found")
	}

	return user, nil
}

func (app *application) CheckAuthentication(w http.ResponseWriter, r *http.Request) {
	// validate the received token from a client, and get the associated user
	user, err := app.authenticateToken(r)
	if err != nil {
		app.invalidCredentials(w)
		return
	}

	// send a response
	var payload struct {
		Error   bool   `json:"error"`
		Message string `json:"message"`
	}
	payload.Error = false
	payload.Message = fmt.Sprintf("Authenticated user %s", user.Email)

	_ = app.writeJSON(w, http.StatusOK, payload)
}

// VirtualTerminalPaymentSucceeded is a handler that takes a posted data from the frontend virtual terminal and saves its transaction to the transactions table in the database
func (app *application) VirtualTerminalPaymentSucceeded(w http.ResponseWriter, r *http.Request) {
	var txnData struct {
		PaymentAmount   int    `json:"amount"`
		PaymentCurrency string `json:"currency"`
		FirstName       string `json:"first_name"`
		LastName        string `json:"last_name"`
		Email           string `json:"email"`
		PaymentIntentID string `json:"payment_intent_id"`
		PaymentMethodID string `json:"payment_method_id"`
		BankReturnCode  string `json:"bank_return_code"`
		ExpiryMonth     int    `json:"expiry_month"`
		ExpiryYear      int    `json:"expiry_year"`
		LastFour        string `json:"last_four"`
	}

	// read posted data
	err := app.readJSON(w, r, &txnData)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	// create a card with a key and a secret
	card := cards.Card{
		Secret: app.config.stripe.secret,
		Key:    app.config.stripe.key,
	}

	// Get the payment intent by payment intent id
	pi, err := card.RetrievePaymentIntent(txnData.PaymentIntentID)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	// Get the payment method details by payment method id
	pm, err := card.RetrievePaymentMethod(txnData.PaymentMethodID)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	// extract information from payment intent(pi) and payment method(pm)
	txnData.BankReturnCode = pi.Charges.Data[0].ID
	txnData.LastFour = pm.Card.Last4
	txnData.ExpiryMonth = int(pm.Card.ExpMonth)
	txnData.ExpiryYear = int(pm.Card.ExpYear)

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

	// save the transaction to the transactions table in the database
	_, err = app.SaveTransaction(txn)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	// write a response
	app.writeJSON(w, http.StatusOK, txn)
}

func (app *application) SendPasswordResetEmail(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Email string `json:"email"`
	}

	err := app.readJSON(w, r, &payload)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	// verify that the posted email exists
	_, err = app.DB.GetUserByEmail(payload.Email)
	if err != nil {
		var resp struct {
			Error   bool   `json:"error"`
			Message string `json:"message"`
		}
		resp.Error = true
		resp.Message = "No matching email found on our system"
		app.writeJSON(w, http.StatusAccepted, resp)
		return
	}

	// construct a signed url for resetting a password
	link := fmt.Sprintf("%s/reset-password?email=%s", app.config.frontend_url, payload.Email)

	sign := urlsigner.Signer{
		Secret: []byte(app.config.pwreset_secretkey),
	}

	signedLink := sign.GenerateTokenFromString(link)

	var data struct {
		Link string
	}

	data.Link = signedLink

	// send an email with a signed password reset link
	err = app.SendMail("info@codingmunger.com", payload.Email, "Password Reset Request", "password-reset", data)
	if err != nil {
		app.errorLog.Println(err)
		app.badRequest(w, r, err)
		return
	}

	var resp struct {
		Error   bool   `json:"error"`
		Message string `json:"message"`
	}

	resp.Error = false

	app.writeJSON(w, http.StatusCreated, resp)
}

func (app *application) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	err := app.readJSON(w, r, &payload)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	// decrypt the email
	encryptor := encryption.Encryption{
		Key: []byte(app.config.pwreset_secretkey),
	}

	email, err := encryptor.Decrypt(payload.Email)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	user, err := app.DB.GetUserByEmail(email)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(payload.Password), 12)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	err = app.DB.UpdatePasswordForUser(user, string(newHash))
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	var resp struct {
		Error   bool   `json:"error"`
		Message string `json:"message"`
	}

	resp.Error = false
	resp.Message = "password changed"

	app.writeJSON(w, http.StatusCreated, resp)
}

func (app *application) AllSales(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		PageSize    int `json:"page_size"`
		CurrentPage int `json:"current_page"`
	}

	err := app.readJSON(w, r, &payload)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	// get all paginated orders
	allSales, lastPage, totalRecords, err := app.DB.GetAllOrdersPaginated(payload.PageSize, payload.CurrentPage)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	var resp struct {
		Currentpage  int             `json:"current_page"`
		PageSize     int             `json:"page_size"`
		LastPage     int             `json:"last_page"`
		TotalRecords int             `json:"total_records"`
		Orders       []*models.Order `json:"orders"`
	}

	resp.Currentpage = payload.CurrentPage
	resp.PageSize = payload.PageSize
	resp.LastPage = lastPage
	resp.TotalRecords = totalRecords
	resp.Orders = allSales

	app.writeJSON(w, http.StatusOK, resp)
}

func (app *application) AllSubscriptions(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		PageSize    int `json:"page_size"`
		CurrentPage int `json:"current_page"`
	}

	err := app.readJSON(w, r, &payload)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	// get all paginated orders
	allSubscriptions, lastPage, totalRecords, err := app.DB.GetAllSubscriptionsPaginated(payload.PageSize, payload.CurrentPage)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	var resp struct {
		Currentpage  int             `json:"current_page"`
		PageSize     int             `json:"page_size"`
		LastPage     int             `json:"last_page"`
		TotalRecords int             `json:"total_records"`
		Orders       []*models.Order `json:"orders"`
	}

	resp.Currentpage = payload.CurrentPage
	resp.PageSize = payload.PageSize
	resp.LastPage = lastPage
	resp.TotalRecords = totalRecords
	resp.Orders = allSubscriptions

	app.writeJSON(w, http.StatusOK, resp)
}

func (app *application) GetOrder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	orderID, _ := strconv.Atoi(id)

	order, err := app.DB.GetOrderByID(orderID)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, order)
}

func (app *application) RefundCharge(w http.ResponseWriter, r *http.Request) {
	var chargeToRefund struct {
		ID            int    `json:"id"`
		PaymentIntent string `json:"payment_intent"`
		Amount        int    `json:"amount"`
		Currency      string `json:"currency"`
	}

	// TODO: option to implement validation here

	err := app.readJSON(w, r, &chargeToRefund)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	// create an card instance of Card type
	card := cards.Card{
		Secret:   app.config.stripe.secret,
		Key:      app.config.stripe.key,
		Currency: chargeToRefund.Currency,
	}

	err = card.Refund(chargeToRefund.PaymentIntent, chargeToRefund.Amount)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	// update the status_id in the orders table from '1: cleared' to '2: refunded'
	err = app.DB.UpdateOrderStatus(chargeToRefund.ID, 2)
	if err != nil {
		app.badRequest(w, r, errors.New("the charge was refunded, but the database could not be updated to indicate the refunded status"))
		return
	}

	var resp struct {
		Error   bool   `json:"error"`
		Message string `json:"message"`
	}
	resp.Error = false
	resp.Message = "Charge refunded"

	app.writeJSON(w, http.StatusOK, resp)
}

func (app *application) CancelSubscription(w http.ResponseWriter, r *http.Request) {
	var subToCancel struct {
		ID             int    `json:"id"`
		SubscriptionID string `json:"payment_intent"`
		Currency       string `json:"currency"`
	}

	// TODO: option to implement validation here

	err := app.readJSON(w, r, &subToCancel)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	// create an card instance of Card type
	card := cards.Card{
		Secret:   app.config.stripe.secret,
		Key:      app.config.stripe.key,
		Currency: subToCancel.Currency,
	}

	err = card.CancelSubscription(subToCancel.SubscriptionID)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	// update the status_id in the orders table from '1 (cleared)' to '3 (cancelled)'
	err = app.DB.UpdateOrderStatus(subToCancel.ID, 3)
	if err != nil {
		app.badRequest(w, r, errors.New("the subcription was cancelled, but the database could not be updated to indicate the cancelled status"))
		return
	}

	var resp struct {
		Error   bool   `json:"error"`
		Message string `json:"message"`
	}
	resp.Error = false
	resp.Message = "Subscription cancelled"

	app.writeJSON(w, http.StatusOK, resp)
}

func (app *application) AllUsers(w http.ResponseWriter, r *http.Request) {
	allUsers, err := app.DB.GetAllUsers()
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, allUsers)
}

func (app *application) OneUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID, _ := strconv.Atoi(id)

	user, err := app.DB.GetOneUser(userID)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, user)
}