# Twilio Provider

Provider for sending emails and SMS via [Twilio](https://twilio.com).

## Configuration

| Field        | Type   | Required | Description            |
|--------------|--------|----------|------------------------|
| `accountSid` | string | Yes      | Your Twilio Account SID |
| `authToken`  | string | Yes      | Your Twilio Auth Token  |
| `messagingServiceSid` | string | No | Twilio Messaging Service SID used for SMS |
| `fromPhone` | string | No | Default SMS sender number (E.164) |
| `sendGridApiKey` | string | Email only | SendGrid API key for the email channel |
| `fromEmail` | string | No | Default sender email if template does not provide one |
| `fromName` | string | No | Default sender display name |

## Usage

1. Get your Account SID and Auth Token from the [Twilio Console](https://console.twilio.com)
2. For SMS, configure either `fromPhone` or `messagingServiceSid`
3. For email, provide `sendGridApiKey`
4. Configure the provider with your credentials
