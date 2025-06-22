package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
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

	json.NewEncoder(w).Encode(AppointmentResponse{Slots: slotsWithStatus})
}

func handlePostAppointment(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	var appointment Appointment
	if err := json.NewDecoder(r.Body).Decode(&appointment); err != nil {
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
		handleError(w, "Error creating appointment", http.StatusInternalServerError)
		return
	}

	appointment.ID = id
	json.NewEncoder(w).Encode(AppointmentResponse{
		Success:     true,
		Message:     "Appointment booked successfully",
		Appointment: appointment,
	})
}

func handleError(w http.ResponseWriter, message string, status int) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(AppointmentResponse{
		Error: message,
	})
}

func main() {
	http.HandleFunc("/api/appointments", handleAppointments)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
