package main

import (
	"fmt"
	"net/http"
	"time"
)

// Order is the type for all orders
type Order struct {
	ID        int       `json:"id"`
	Product   string    `json:"product"`
	Quantity  int       `json:"quantity"`
	Amount    int       `json:"amount"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

func (app *application) CreateAndSendInvoice(w http.ResponseWriter, r *http.Request) {
	// receive json payload
	var order Order

	err := app.readJSON(w, r, &order)
	if err != nil {
		app.badRequest(w, r, err)
		return
	}

	// generate a pdf invoice

	// create an email

	// send an email with a pdf attachment

	// send a response
	var resp struct {
		Error   bool   `json:"error"`
		Message string `json:"message"`
	}
	resp.Error = false
	resp.Message = fmt.Sprintf("Invoice %d.pdf created and sent to %s", order.ID, order.Email)
	app.writeJSON(w, http.StatusOK, resp)
}
