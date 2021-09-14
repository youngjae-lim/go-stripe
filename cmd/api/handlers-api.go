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
	"github.com/youngjae-lim/go-stripe/internal/models"
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
