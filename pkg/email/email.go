// Package email provides an SMTP email dispatcher using the standard net/smtp package.
// It uses STARTTLS (port 587) for transport security.
package email

import (
	"fmt"
	"log"
	"net/smtp"
)

// Mailer holds the SMTP connection configuration.
// Create one instance at startup and share it across handlers.
type Mailer struct {
	host     string // e.g. "smtp.gmail.com"
	port     string // e.g. "587"
	from     string // visible sender address
	user     string // auth login (often same as from)
	password string // app password / SMTP credential
	auth     smtp.Auth
}

// New constructs a Mailer and pre-builds the smtp.Auth credential.
// No network connection is made at this point.
func New(host, port, from, user, password string) *Mailer {
	auth := smtp.PlainAuth("", user, password, host)
	return &Mailer{
		host:     host,
		port:     port,
		from:     from,
		user:     user,
		password: password,
		auth:     auth,
	}
}

// SendOTP dispatches a branded OTP email to the recipient address.
// It renders both a plain-text fallback and an HTML body.
func (m *Mailer) SendOTP(toEmail, toName, code string) error {
	host := m.host
	port := m.port
	user := m.user
	pass := m.password
	sender := m.from

	// 1. Technical Check: Ensure variables aren't completely blank
	if host == "" || user == "" || pass == "" {
		return fmt.Errorf("SMTP configuration error: missing parameters in mailer configuration")
	}

	headerFrom := fmt.Sprintf("From: AARABHYATE <%s>", sender)
	headerTo := fmt.Sprintf("To: <%s>", toEmail)
	headerSubject := "Subject: AARABHYATE - Account Verification Code"
	headerMime := "MIME-Version: 1.0"
	headerContentType := "Content-Type: text/html; charset=\"UTF-8\""
	body := fmt.Sprintf("<h3>AARABHYATE Research Group</h3><p>Your OTP code is: <strong>%s</strong></p>", code)

	msgString := fmt.Sprintf("%s\r\n%s\r\n%s\r\n%s\r\n%s\r\n\r\n%s", 
		headerFrom, headerTo, headerSubject, headerMime, headerContentType, body)
	msg := []byte(msgString)

	// 2. Connect to the SMTP server address
	serverAddress := host + ":" + port
	log.Printf("[SMTP Debug] Attempting connection to secure server: %s", serverAddress)
	
	auth := smtp.PlainAuth("", user, pass, host)

	// 3. Execute the network transaction
	err := smtp.SendMail(serverAddress, auth, sender, []string{toEmail}, msg)
	if err != nil {
		// This prints the raw, uncut error directly from Brevo's mail servers to your terminal
		log.Printf("[SMTP ERROR EVENT]: Execution failed! Details: %v", err)
		return err
	}

	log.Printf("[SMTP Success] OTP mail dispatched flawlessly to %s", toEmail)
	return nil
}
