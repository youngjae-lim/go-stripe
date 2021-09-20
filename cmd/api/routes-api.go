package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

func (app *application) routes() http.Handler {
	mux := chi.NewRouter()

	mux.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: false,
		MaxAge:           300, // 5 mins
	}))

	mux.Post("/api/payment-intent", app.GetPaymentIntent)
	mux.Get("/api/widget/{id}", app.GetWidgetByID)

	mux.Post("/api/subscribe", app.SubscribeToPlan)

	mux.Post("/api/authenticate", app.CreateAuthToken)
	mux.Post("/api/is-authenticated", app.CheckAuthentication)
	mux.Post("/api/forgot-password", app.SendPasswordResetEmail)
	mux.Post("/api/reset-password", app.ResetPassword)

	// protected routes
	mux.Route("/api/admin", func(mux chi.Router) {
		mux.Use(app.Auth)

		mux.Post("/virtual-terminal-succeeded", app.VirtualTerminalPaymentSucceeded)
		mux.Post("/all-sales", app.AllSales)
		mux.Post("/all-subscriptions", app.AllSubscriptions)
		// this route works for getting an order for both general widget sales and subscriptions
		mux.Get("/get-order/{id}", app.GetOrder)
		mux.Post("/refund", app.RefundCharge)
		mux.Post("/cancel-subscription", app.CancelSubscription)

		mux.Get("/all-users", app.AllUsers)
		mux.Post("/all-users/{id}", app.OneUser)
	})

	return mux
}
