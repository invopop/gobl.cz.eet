package eet

// Environment is one of the EET service's two environments, production or
// the Playground, and gives both the URL to send a sale to and the signer
// its confirmations must have.
type Environment string

// The service's environments.
const (
	EnvironmentProduction Environment = "production"
	EnvironmentPlayground Environment = "playground"
)

// URL is where the environment receives sales ("Přístupové a provozní
// informace" v1.2, chapter 2.1), or empty for an unknown environment.
func (e Environment) URL() string {
	switch e {
	case EnvironmentProduction:
		return "https://trzbyeet.gov.cz/eet/services/EETServiceSOAP/v4"
	case EnvironmentPlayground:
		return "https://pg.trzbyeet.gov.cz/eet/services/EETServiceSOAP/v4"
	}
	return ""
}

// trust is how the environment's confirmations are checked, or nil for an
// unknown environment.
func (e Environment) trust() *trust {
	switch e {
	case EnvironmentProduction:
		return defaultTrust(false)
	case EnvironmentPlayground:
		return defaultTrust(true)
	}
	return nil
}
