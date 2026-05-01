# Aegion Email Templates

This directory contains HTML email templates for the Aegion identity platform. All templates use Go's `html/template` syntax and are designed to be responsive, accessible, and brand-neutral.

## Directory Structure

```
templates/
├── email/
│   ├── base.html          # Base template with common styling
│   ├── verification.html  # Email verification
│   ├── recovery.html      # Password reset
│   ├── magic_link.html    # Passwordless login
│   └── welcome.html       # Welcome email after registration
└── README.md              # This file
```

## Templates

### Base Template (`base.html`)

The base template provides common styling and structure. It can be used to wrap custom content.

| Variable | Type | Description |
|----------|------|-------------|
| `.Title` | string | Email title (displayed in header) |
| `.Content` | template.HTML | Main content block (HTML) |
| `.Footer` | string | Optional footer text |
| `.Year` | int | Current year for copyright |

### Verification Template (`verification.html`)

Sent when a user needs to verify their email address.

| Variable | Type | Required | Description |
|----------|------|----------|-------------|
| `.Name` | string | No | User's name or email |
| `.Code` | string | Yes | 6-digit verification code |
| `.Link` | string | No | Verification URL (if using link-based verification) |
| `.ExpiresIn` | string | No | Expiration time (default: "15 minutes") |
| `.Year` | int | Yes | Current year |

### Recovery Template (`recovery.html`)

Sent for password reset requests.

| Variable | Type | Required | Description |
|----------|------|----------|-------------|
| `.Name` | string | No | User's name or email |
| `.Code` | string | No | Reset code (if using code-based reset) |
| `.Link` | string | Yes | Password reset URL |
| `.ExpiresIn` | string | No | Expiration time (default: "1 hour") |
| `.IP` | string | No | IP address of the requester |
| `.Year` | int | Yes | Current year |

### Magic Link Template (`magic_link.html`)

Sent for passwordless authentication.

| Variable | Type | Required | Description |
|----------|------|----------|-------------|
| `.Name` | string | No | User's name or email |
| `.Link` | string | Yes | Magic link URL |
| `.ExpiresIn` | string | No | Expiration time (default: "10 minutes") |
| `.IP` | string | No | IP address of the requester |
| `.Device` | string | No | Device/browser information |
| `.Year` | int | Yes | Current year |

### Welcome Template (`welcome.html`)

Sent after successful registration.

| Variable | Type | Required | Description |
|----------|------|----------|-------------|
| `.Name` | string | No | User's name |
| `.Email` | string | No | User's email address |
| `.Link` | string | No | Dashboard or getting started URL |
| `.Features` | []string | No | List of key features to highlight |
| `.SupportEmail` | string | No | Support contact email (default: "support@aegion.io") |
| `.Year` | int | Yes | Current year |

## Usage Example

```go
package main

import (
    "bytes"
    "html/template"
    "time"
)

func RenderVerificationEmail(name, code, link string) (string, error) {
    tmpl, err := template.ParseFiles("templates/email/verification.html")
    if err != nil {
        return "", err
    }

    data := map[string]interface{}{
        "Name":      name,
        "Code":      code,
        "Link":      link,
        "ExpiresIn": "15 minutes",
        "Year":      time.Now().Year(),
    }

    var buf bytes.Buffer
    if err := tmpl.Execute(&buf, data); err != nil {
        return "", err
    }

    return buf.String(), nil
}
```

## Customization

### Styling

All templates use inline CSS for maximum email client compatibility. To customize:

1. **Colors**: Search and replace the following color values:
   - Primary blue: `#2563eb` (buttons, links, header)
   - Dark blue: `#1d4ed8` (hover states)
   - Text dark: `#1f2937` (headings)
   - Text gray: `#4b5563` (body text)
   - Text muted: `#6b7280` (secondary text)
   - Background: `#f4f7fa` (wrapper)
   - White: `#ffffff` (container)

2. **Logo**: Add your logo in the `.email-header` section:
   ```html
   <img src="https://your-domain.com/logo.png" alt="Your Logo" width="150" height="40">
   ```

3. **Fonts**: The templates use system fonts by default. To use custom fonts, add a web font import (note: limited email client support).

### Adding New Templates

1. Create a new HTML file in `templates/email/`
2. Copy the structure from an existing template
3. Define your variables using Go template syntax: `{{.VariableName}}`
4. Include a plain text version in HTML comments
5. Document the template and variables in this README

### Plain Text Fallback

Each template includes a plain text version in HTML comments. For proper multipart email support, extract these and send as `text/plain` alongside `text/html`:

```go
// Example: sending multipart email
msg := gomail.NewMessage()
msg.SetHeader("From", "no-reply@aegion.io")
msg.SetHeader("To", recipient)
msg.SetHeader("Subject", subject)
msg.SetBody("text/plain", plainTextContent)
msg.AddAlternative("text/html", htmlContent)
```

## Accessibility

All templates follow accessibility best practices:

- Semantic HTML structure
- Proper heading hierarchy
- Sufficient color contrast (WCAG 2.1 AA)
- Descriptive `alt` text for images
- `role="presentation"` on layout tables
- `role="button"` on CTA links
- Clear, readable font sizes (minimum 14px)

## Email Client Compatibility

These templates are tested for compatibility with:

- Gmail (Web, iOS, Android)
- Apple Mail (macOS, iOS)
- Outlook (2016+, Web, Mobile)
- Yahoo Mail
- Samsung Mail
- Thunderbird

### Known Limitations

- **Outlook (Windows)**: Gradients may not render; falls back to solid color
- **Gmail**: Some CSS properties may be stripped; critical styles are inline
- **Dark Mode**: Basic dark mode support; some clients may invert colors

## Security Considerations

1. **Never include sensitive data** (passwords, tokens) directly in emails
2. **Use short expiration times** for verification codes and magic links
3. **Include IP/device info** to help users identify suspicious activity
4. **Use HTTPS links** for all URLs
5. **Validate template data** to prevent injection attacks

## License

These templates are part of the Aegion project and are subject to the project's license terms.
