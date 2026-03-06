package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/extism/go-pdk"
	pdkhttp "github.com/extism/go-pdk/http"
	"github.com/lunogram/platform/pkg/modules"
	"github.com/lunogram/platform/pkg/modules/providers"
)

const (
	twilioAPIBase   = "https://api.twilio.com"
	sendGridAPIBase = "https://api.sendgrid.com"
)

//go:export manifest
func Manifest() int32 {
	manifest := providers.ProviderManifest{
		Metadata: modules.Metadata{
			ID:          "twilio",
			Title:       "Twilio",
			Description: "Send emails and SMS via Twilio",
			Tags:        []string{"email", "sms"},
		},
		Website: "https://twilio.com",
		Version: "1.0.0",
		License: "MIT",
		Author: modules.Author{
			Name:  "Lunogram",
			Email: "dev@lunogram.io",
			URL:   "https://lunogram.com",
		},
		Spec: providers.ProviderSpec{
			Channels: []providers.Channel{
				providers.ChannelEmail,
				providers.ChannelSMS,
			},
			Config: &modules.JSONSchema{
				Type: "object",
				Properties: []modules.JSONSchemaProperty{
					{
						Name: "data",
						Schema: &modules.JSONSchema{
							Type: "object",
							Properties: []modules.JSONSchemaProperty{
								{
									Name:   "accountSid",
									Schema: &modules.JSONSchema{Type: "string", Title: "Account SID"},
								},
								{
									Name:   "authToken",
									Schema: &modules.JSONSchema{Type: "string", Title: "Auth Token", Format: "password"},
								},
								{
									Name:   "default_from",
									Schema: &modules.JSONSchema{Type: "string", Title: "Default From Number", Description: "Default sender phone number (for SMS) or email address (for email)"},
								},
								{
									Name:   "default_from_name",
									Schema: &modules.JSONSchema{Type: "string", Title: "Default From Name", Description: "Default sender display name (email only)"},
								},
								{
									Name:   "default_from_locked",
									Schema: &modules.JSONSchema{Type: "boolean", Title: "Lock From", Description: "Prevent templates from overriding the from value"},
								},
							},
							Required: []string{"accountSid", "authToken"},
						},
					},
				},
			},
		},
	}

	err := pdk.OutputJSON(manifest)
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	return 0
}

type Config struct {
	AccountSID          string `json:"accountSid"`
	AuthToken           string `json:"authToken"`
	MessagingServiceSID string `json:"messagingServiceSid"`
	FromPhone           string `json:"fromPhone"`
	SendGridAPIKey      string `json:"sendGridApiKey"`
	FromEmail           string `json:"fromEmail"`
	FromName            string `json:"fromName"`
}

//go:export send
func Send() int32 {
	var req providers.SendRequest[Config]
	err := pdk.InputJSON(&req)
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	switch req.Channel {
	case providers.ChannelEmail:
		return sendEmail(&req)

	case providers.ChannelSMS:
		return sendSMS(&req)

	default:
		pdk.SetError(fmt.Errorf("unsupported channel: %s", req.Channel))
		return -1
	}
}

func sendEmail(req *providers.SendRequest[Config]) int32 {
	email, err := req.GetEmailPayload()
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	if req.Config.SendGridAPIKey == "" {
		pdk.SetError(fmt.Errorf("missing sendGridApiKey in provider config"))
		return -1
	}

	fromAddress := email.From.Address
	if fromAddress == "" {
		fromAddress = req.Config.FromEmail
	}

	if fromAddress == "" {
		pdk.SetError(fmt.Errorf("missing sender email: set template from.email or provider config fromEmail"))
		return -1
	}

	fromName := email.From.Name
	if fromName == "" {
		fromName = req.Config.FromName
	}

	pdk.Log(pdk.LogInfo, fmt.Sprintf("sending email via Twilio SendGrid"))
	pdk.Log(pdk.LogInfo, fmt.Sprintf("to: %s", email.To))
	pdk.Log(pdk.LogInfo, fmt.Sprintf("subject: %s", email.Subject))

	sendReq := sendGridRequest{
		Personalizations: []sendGridPersonalization{{
			To: []sendGridAddress{{Email: email.To}},
		}},
		From: sendGridAddress{
			Email: fromAddress,
			Name:  fromName,
		},
		Subject: email.Subject,
	}

	if email.Text != "" {
		sendReq.Content = append(sendReq.Content, sendGridContent{Type: "text/plain", Value: email.Text})
	}

	if email.HTML != "" {
		sendReq.Content = append(sendReq.Content, sendGridContent{Type: "text/html", Value: email.HTML})
	}

	if len(sendReq.Content) == 0 {
		pdk.SetError(fmt.Errorf("email payload must include text or html content"))
		return -1
	}

	if email.ReplyTo != nil && *email.ReplyTo != "" {
		sendReq.ReplyTo = &sendGridAddress{Email: *email.ReplyTo}
	}

	if len(email.Headers) > 0 {
		sendReq.Headers = email.Headers
	}

	if email.Cc != nil && *email.Cc != "" {
		sendReq.Personalizations[0].Cc = []sendGridAddress{{Email: *email.Cc}}
	}

	if email.Bcc != nil && *email.Bcc != "" {
		sendReq.Personalizations[0].Bcc = []sendGridAddress{{Email: *email.Bcc}}
	}

	if email.List != nil && email.List.Unsubscribe != "" {
		if sendReq.Headers == nil {
			sendReq.Headers = map[string]string{}
		}
		sendReq.Headers["List-Unsubscribe"] = email.List.Unsubscribe
	}

	body, err := json.Marshal(sendReq)
	if err != nil {
		pdk.SetError(fmt.Errorf("failed to marshal sendgrid request: %w", err))
		return -1
	}

	httpReq, err := http.NewRequest(http.MethodPost, sendGridAPIBase+"/v3/mail/send", bytes.NewReader(body))
	if err != nil {
		pdk.SetError(fmt.Errorf("failed to create sendgrid request: %w", err))
		return -1
	}

	httpReq.Header.Set("Authorization", "Bearer "+req.Config.SendGridAPIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Transport: &pdkhttp.HTTPTransport{}}
	res, err := client.Do(httpReq)
	if err != nil {
		pdk.SetError(fmt.Errorf("sendgrid request failed: %w", err))
		return -1
	}

	defer func() {
		_ = res.Body.Close()
	}()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		apiErr := sendGridError{}
		if err := json.NewDecoder(res.Body).Decode(&apiErr); err != nil {
			pdk.SetError(fmt.Errorf("sendgrid request failed with status %d", res.StatusCode))
			return -1
		}

		pdk.SetError(fmt.Errorf("sendgrid request failed with status %d: %s", res.StatusCode, apiErr.String()))
		return -1
	}

	messageID := res.Header.Get("X-Message-Id")
	if messageID == "" {
		messageID = res.Header.Get("X-Message-ID")
	}

	response := providers.SendResponse{
		ID:     messageID,
		Status: "sent",
		Metadata: map[string]any{
			"channel": req.Channel,
			"to":      email.To,
		},
	}

	err = pdk.OutputJSON(response)
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	return 0
}

func sendSMS(req *providers.SendRequest[Config]) int32 {
	sms, err := req.GetSMSPayload()
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	from := sms.From
	if from == "" {
		from = req.Config.FromPhone
	}

	if from == "" && req.Config.MessagingServiceSID == "" {
		pdk.SetError(fmt.Errorf("missing sender: set sms.from, provider fromPhone, or messagingServiceSid"))
		return -1
	}

	pdk.Log(pdk.LogInfo, fmt.Sprintf("sending SMS via Twilio"))
	pdk.Log(pdk.LogInfo, fmt.Sprintf("to: %s", sms.To))
	pdk.Log(pdk.LogInfo, fmt.Sprintf("from: %s", from))
	pdk.Log(pdk.LogInfo, fmt.Sprintf("body: %s", sms.Body))

	form := url.Values{}
	form.Set("To", sms.To)
	form.Set("Body", sms.Body)

	if from != "" {
		form.Set("From", from)
	}

	if req.Config.MessagingServiceSID != "" {
		form.Set("MessagingServiceSid", req.Config.MessagingServiceSID)
	}

	for _, mediaURL := range sms.MediaURLs {
		if strings.TrimSpace(mediaURL) != "" {
			form.Add("MediaUrl", mediaURL)
		}
	}

	endpoint := fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json", twilioAPIBase, req.Config.AccountSID)
	httpReq, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		pdk.SetError(fmt.Errorf("failed to create twilio request: %w", err))
		return -1
	}

	httpReq.SetBasicAuth(req.Config.AccountSID, req.Config.AuthToken)
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Transport: &pdkhttp.HTTPTransport{}}
	res, err := client.Do(httpReq)
	if err != nil {
		pdk.SetError(fmt.Errorf("twilio request failed: %w", err))
		return -1
	}

	defer func() {
		_ = res.Body.Close()
	}()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		apiErr := twilioError{}
		if err := json.NewDecoder(res.Body).Decode(&apiErr); err != nil {
			pdk.SetError(fmt.Errorf("twilio sms failed with status %d", res.StatusCode))
			return -1
		}

		pdk.SetError(fmt.Errorf("twilio sms failed with status %d: %s", res.StatusCode, apiErr.String()))
		return -1
	}

	sent := twilioMessageResponse{}
	if err := json.NewDecoder(res.Body).Decode(&sent); err != nil {
		pdk.SetError(fmt.Errorf("failed to decode twilio response: %w", err))
		return -1
	}

	response := providers.SendResponse{
		ID:     sent.SID,
		Status: "sent",
		Metadata: map[string]any{
			"channel": req.Channel,
			"to":      sms.To,
		},
	}

	err = pdk.OutputJSON(response)
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	return 0
}

type twilioMessageResponse struct {
	SID string `json:"sid"`
}

type twilioError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e twilioError) String() string {
	if e.Code == 0 {
		return e.Message
	}
	return fmt.Sprintf("%d: %s", e.Code, e.Message)
}

type sendGridRequest struct {
	Personalizations []sendGridPersonalization `json:"personalizations"`
	From             sendGridAddress           `json:"from"`
	ReplyTo          *sendGridAddress          `json:"reply_to,omitempty"`
	Subject          string                    `json:"subject"`
	Content          []sendGridContent         `json:"content"`
	Headers          map[string]string         `json:"headers,omitempty"`
}

type sendGridPersonalization struct {
	To  []sendGridAddress `json:"to"`
	Cc  []sendGridAddress `json:"cc,omitempty"`
	Bcc []sendGridAddress `json:"bcc,omitempty"`
}

type sendGridAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type sendGridContent struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type sendGridError struct {
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (e sendGridError) String() string {
	if len(e.Errors) == 0 {
		return "unknown error"
	}

	parts := make([]string, 0, len(e.Errors))
	for _, item := range e.Errors {
		if item.Message != "" {
			parts = append(parts, item.Message)
		}
	}

	if len(parts) == 0 {
		return "unknown error"
	}

	return strings.Join(parts, "; ")
}

func main() {}
