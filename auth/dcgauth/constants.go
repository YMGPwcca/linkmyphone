package dcgauth

import "time"

const (
	// Production service defaults from PlatformConfiguration.
	ProdServiceBase = "https://dcg.microsoft.com/"
	RelayHubEndpoint = "relayhub/"

	// MSA resource scopes used for DCG account bootstrap.
	LegacyProdMSAScope   = "service::msatoken.dcg.microsoft.com::MBI_SSL"
	MigratedProdMSAScope = "https://dcg.microsoft.com/DCG.ReadWrite"

	// Services tokens used by SignalR and service requests are requested
	// against the established DCG identity with this scope.
	ServicesScopeGeneral = "general"

	AuthAPIVersion             = "1.1.0"
	DeviceManagementAPIVersion = "1.5.0"

	// Public CrossDevice Microsoft-account client/application id.
	MSAClientID = "ca3b40e4-3001-4842-8f21-49c0045404f8"
	MSAAppID    = MSAClientID

	HeaderUserIdentityType  = "UserIdentityType"
	HeaderUserIdentityToken = "UserIdentityToken"
	HeaderAuthorizationType = "Authorization-Type"
	UserIdentityTypeMSA     = "MSA"
)

const (
	DefaultCertificateClockDrift = 12 * time.Hour
	DefaultCertificateLifetime   = 365 * 24 * time.Hour
	DefaultJWTClockDrift         = 12 * time.Hour
	DefaultJWTLifetime           = 12 * time.Hour
	DefaultHeartbeatFrequency    = 15 * time.Second
)
