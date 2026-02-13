package config

import (
	"github.com/spf13/viper"
)

// Init sets up Viper with defaults and config file search paths.
// It does not fail if no config file is found — that's handled in later stories.
func Init() {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("$HOME/.config/jc")
	viper.SetEnvPrefix("JC")
	viper.AutomaticEnv()

	// Read config file if present; ignore if missing.
	_ = viper.ReadInConfig()
}
