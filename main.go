package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Constants
var (
	AVAILABLE_DAYS = []int{2, 4, 5} // Tuesday (2), Thursday (4), Friday (5)
	BLOCKED_TIME   = struct {
		Start string
		End   string
	}{
		Start: "14:00",
		End:   "14:30",
	}
)

// Structs for request/response handling
type TimeSlot struct {
	Time     string `json:"time"`
	IsBooked bool   `json:"isBooked"`
}

type Appointment struct {
	ID      int64   `json:"id"`
	Name    string  `json:"name"`
	Email   string  `json:"email"`
	Date    string  `json:"date"`
	Time    string  `json:"time"`
	Message *string `json:"message,omitempty"`
}

type AppointmentResponse struct {
	Success     bool        `json:"success,omitempty"`
	Message     string      `json:"message,omitempty"`
	Error       string      `json:"error,omitempty"`
	Slots       []TimeSlot  `json:"slots,omitempty"`
	Appointment Appointment `json:"appointment,omitempty"`
}

type ContactRequest struct {
	FullName string `json:"fullName"`
	Email    string `json:"email"`
}

// generateTimeSlots generates available time slots from 9:00 to 17:00
func generateTimeSlots() []string {
	var slots []string
	for hour := 9; hour < 17; hour++ {
		for minute := 0; minute < 60; minute += 30 {
			timeString := fmt.Sprintf("%02d:%02d", hour, minute)

			// Skip blocked time (14:00-14:30)
			if timeString >= BLOCKED_TIME.Start && timeString < BLOCKED_TIME.End {
				continue
			}

			slots = append(slots, timeString)
		}
	}
	return slots
}

// contains checks if a slice contains a value
func contains(slice []int, item int) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

func handleAppointments(w http.ResponseWriter, r *http.Request) {
	log.Info().Str("method", r.Method).Str("path", r.URL.Path).Msg("Received request for appointments")
	w.Header().Set("Content-Type", "application/json")

	// Get database connection string from environment variable
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		handleError(w, "DATABASE_URL environment variable not set", http.StatusInternalServerError)
		return
	}

	// Initialize database connection
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		handleError(w, "Database connection error", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	switch r.Method {
	case http.MethodGet:
		handleGetAppointments(w, r, db)
	case http.MethodPost:
		handlePostAppointment(w, r, db)
	default:
		handleError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleGetAppointments(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	date := r.URL.Query().Get("date")
	log.Info().Str("date", date).Msg("Handling GET appointments request")
	if date == "" {
		handleError(w, "Date is required", http.StatusBadRequest)
		return
	}

	// Parse and validate date
	parsedDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		handleError(w, "Invalid date format", http.StatusBadRequest)
		return
	}

	dayOfWeek := int(parsedDate.Weekday())
	if !contains(AVAILABLE_DAYS, dayOfWeek) {
		json.NewEncoder(w).Encode(AppointmentResponse{Slots: []TimeSlot{}})
		return
	}

	// Get booked appointments
	rows, err := db.Query("SELECT time FROM appointments WHERE date = $1", date)
	if err != nil {
		handleError(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	bookedTimes := make(map[string]bool)
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			handleError(w, "Database error", http.StatusInternalServerError)
			return
		}
		// Normalize time format (remove seconds if present)
		if t == "" {
			handleError(w, "Time of booked appointment was empty", http.StatusInternalServerError)
		}
		splittedString := strings.Split(t, "T")
		if len(splittedString) != 2 {
			handleError(w, "Time string was not in correct format with a T as a separator. Please clean the data!", http.StatusInternalServerError)
		}
		t = splittedString[1]

		if len(t) > 5 {
			t = t[:5]
		}
		bookedTimes[t] = true
	}

	// Generate all slots with booking status
	allSlots := generateTimeSlots()
	slotsWithStatus := make([]TimeSlot, len(allSlots))
	for i, slot := range allSlots {
		slotsWithStatus[i] = TimeSlot{
			Time:     slot,
			IsBooked: bookedTimes[slot],
		}
	}

	log.Info().Int("available_slots", len(slotsWithStatus)).Str("date", date).Msg("Successfully retrieved appointment slots")
	json.NewEncoder(w).Encode(AppointmentResponse{Slots: slotsWithStatus})
}

func handlePostAppointment(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var appointment Appointment
	log.Info().Msg("Handling POST appointment request")
	if err := json.NewDecoder(r.Body).Decode(&appointment); err != nil {
		log.Error().Err(err).Msg("Failed to decode request body")
		handleError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if appointment.Name == "" || appointment.Email == "" ||
		appointment.Date == "" || appointment.Time == "" {
		handleError(w, "Name, email, date, and time are required", http.StatusBadRequest)
		return
	}

	// Email validation
	emailRegex := regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	if !emailRegex.MatchString(appointment.Email) {
		handleError(w, "Invalid email format", http.StatusBadRequest)
		return
	}

	// Parse and validate date
	parsedDate, err := time.Parse("2006-01-02", appointment.Date)
	if err != nil {
		handleError(w, "Invalid date format", http.StatusBadRequest)
		return
	}

	dayOfWeek := int(parsedDate.Weekday())
	if !contains(AVAILABLE_DAYS, dayOfWeek) {
		handleError(w, "This day is not available for appointments", http.StatusBadRequest)
		return
	}

	// Check if time slot is available
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM appointments WHERE date = $1 AND time = $2",
		appointment.Date, appointment.Time).Scan(&count)
	if err != nil {
		handleError(w, "Database error", http.StatusInternalServerError)
		return
	}
	if count > 0 {
		handleError(w, "This time slot is already booked", http.StatusBadRequest)
		return
	}

	// Create the appointment
	var id int64
	err = db.QueryRow(`
		INSERT INTO appointments (name, email, date, time, message)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		strings.TrimSpace(appointment.Name),
		strings.ToLower(strings.TrimSpace(appointment.Email)),
		appointment.Date,
		appointment.Time,
		appointment.Message,
	).Scan(&id)

	if err != nil {
		log.Error().Err(err).Msg("Error creating appointment")
		handleError(w, "Error creating appointment", http.StatusInternalServerError)
		return
	}

	appointment.ID = id
	log.Info().
		Str("name", appointment.Name).
		Str("email", appointment.Email).
		Str("date", appointment.Date).
		Str("time", appointment.Time).
		Msg("Appointment booked successfully")
	json.NewEncoder(w).Encode(AppointmentResponse{
		Success:     true,
		Message:     "Appointment booked successfully",
		Appointment: appointment,
	})
}

func handleContacts(w http.ResponseWriter, r *http.Request) {
	log.Info().Str("method", r.Method).Str("path", r.URL.Path).Msg("Received request for contacts")
	w.Header().Set("Content-Type", "application/json")

	// Get database connection string from environment variable
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		handleError(w, "DATABASE_URL environment variable not set", http.StatusInternalServerError)
		return
	}

	// Initialize database connection
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		handleError(w, "Database connection error", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	if r.Method != http.MethodPost {
		handleError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var contact ContactRequest
	if err := json.NewDecoder(r.Body).Decode(&contact); err != nil {
		log.Error().Err(err).Msg("Failed to decode contact request body")
		handleError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if contact.FullName == "" || contact.Email == "" {
		handleError(w, "Full name and email are required", http.StatusBadRequest)
		return
	}

	// Email validation
	emailRegex := regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	if !emailRegex.MatchString(contact.Email) {
		handleError(w, "Invalid email format", http.StatusBadRequest)
		return
	}

	_, err = db.Exec(`INSERT INTO contacts (name, email) VALUES ($1, $2)`, contact.FullName, contact.Email)
	if err != nil {
		log.Error().Err(err).Str("email", contact.Email).Msg("Error saving contact")
		handleError(w, "Error saving contact", http.StatusInternalServerError)
		return
	}
	log.Info().Str("email", contact.Email).Msg("Contact saved successfully")

	// Send email with PDF attachment
	if err := SendMail(contact.Email, contact.FullName); err != nil {
		// Log the email error but don't fail the request,
		// as the contact has already been saved.
		log.Error().Err(err).Str("email", contact.Email).Msg("Failed to send contact email")
	}

	w.WriteHeader(http.StatusOK)
	log.Info().Str("email", contact.Email).Msg("Contact request processed successfully")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Contact received and email sent.",
	})
}

func handleError(w http.ResponseWriter, message string, status int) {
	log.Error().Int("status", status).Msg(message)
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(AppointmentResponse{
		Error: message,
	})
}

// corsMiddleware wraps a handler to add CORS headers
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get the allowed origin from an environment variable
		allowedOrigin := os.Getenv("CORS_ALLOWED_ORIGIN")
		if allowedOrigin == "" {
			// Fallback to a default or handle the error if not set
			// For development, you might use a local URL. For production, this should be your frontend's URL.
			log.Warn().Msg("CORS_ALLOWED_ORIGIN environment variable not set.")
		}

		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Call the next handler in the chain
		next.ServeHTTP(w, r)
	})
}

func main() {
	// Initialize logger
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	// Pretty logging for development
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	// Wrap the original handler with the CORS middleware
	appointmentsHandler := http.HandlerFunc(handleAppointments)
	http.Handle("/api/appointments", corsMiddleware(appointmentsHandler))

	contactsHandler := http.HandlerFunc(handleContacts)
	http.Handle("/api/contacts", corsMiddleware(contactsHandler))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Info().Str("port", port).Msg("Starting server")
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal().Err(err).Msg("Server failed to start")
	}
}



