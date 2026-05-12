package config

// KiroDomains lists every hostname kiroctl hijacks via /etc/hosts.
// Keep in sync with https://kiro.dev/docs/cli/privacy-and-security/firewalls/
//
// Wildcards are expanded to concrete subdomains — /etc/hosts doesn't support
// wildcards. When Kiro adds a new subdomain under *.kiro.dev, append it here
// and run `kiroctl enable` again.
var KiroDomains = []string{
	// Kiro control plane
	"app.kiro.dev",
	"cli.kiro.dev",
	"prod.us-east-1.auth.desktop.kiro.dev",
	"prod.us-east-1.telemetry.desktop.kiro.dev",
	"prod.download.desktop.kiro.dev",
	"prod.download.cli.kiro.dev",
	"runtime.us-east-1.kiro.dev",
	"runtime.eu-central-1.kiro.dev",
	"management.us-east-1.kiro.dev",
	"management.eu-central-1.kiro.dev",
	"telemetry.us-east-1.kiro.dev",
	"telemetry.eu-central-1.kiro.dev",

	// AWS Q service backends
	"q.us-east-1.amazonaws.com",
	"q.eu-central-1.amazonaws.com",
	"desktop-release.q.us-east-1.amazonaws.com",
	"cognito-identity.us-east-1.amazonaws.com",
	"oidc.us-east-1.amazonaws.com",
	"oidc.eu-central-1.amazonaws.com",

	// IAM Identity Center / SSO
	"us-east-1.signin.aws",
	"eu-central-1.signin.aws",
	"signin.aws.amazon.com",

	// External IdPs
	"login.microsoftonline.com",

	// Billing
	"billing.stripe.com",
	"checkout.stripe.com",

	// IDE extension marketplace
	"open-vsx.org",
	"openvsx.eclipsecontent.org",

	// MCP / Powers
	"github.com",
	"api.github.com",
	"raw.githubusercontent.com",
	"codeload.github.com",
	"objects.githubusercontent.com",
}
