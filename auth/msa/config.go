package msa

import "fmt"

const (
	// CrossDevice PlatformConfigureOptions hard-codes the same first-party
	// application identifier for both the WAM client ID and MSA app ID.
	ClientID = "ca3b40e4-3001-4842-8f21-49c0045404f8"
	AppID    = ClientID

	AccountProvider = "https://login.microsoft.com"
	AccountTenant   = "consumers"

	APIVersion = "2.0"
)

// FullScope mirrors MsaTokenProvider's Windows WAM request string:
// <scope>&api-version=2.0&clientid=<MsaAppId>
//
// This is the broker-facing scope string used by the Windows implementation;
// it is not an assertion that every generic OAuth client accepts the same
// string unchanged.
func FullScope(scope string) string {
	return fmt.Sprintf("%s&api-version=%s&clientid=%s", scope, APIVersion, AppID)
}
