package config

// Values other than the build version come exclusively from the managed runtime.
var buildVersion = "2.0.0"

type Config struct {
	ManagedConnection *ManagedConnection `json:"-"`
	Addr              string
	Version           string
	DatabasePath      string
	FrontendOrigin    string
}
type ManagedConnection struct {
	Host  string
	Port  int
	Token string `json:"-"`
}

func Load() Config { return Config{Version: buildVersion} }
