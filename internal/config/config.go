// Package config loads sudiviz settings via Viper (file, env, flags).
package config

import (
	"strings"

	"github.com/spf13/viper"
)

// Settings are the resolved runtime options. AWS credentials are never
// accepted here — they come from the standard SDK chain.
type Settings struct {
	Profile    string
	Region     string
	VPCID      string
	ServiceTag string
	Verbose    bool
	ConfigFile string
}

const envPrefix = "SUDIVIZ"

// Load binds Viper to a config file (optional), environment variables, and
// already-set flag values. Call after Cobra has parsed flags.
func Load() Settings {
	viper.SetEnvPrefix(envPrefix)
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	viper.AutomaticEnv()

	cfgFile := viper.GetString("config")
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("sudiviz")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.config/sudiviz")
		viper.AddConfigPath("$HOME")
	}
	_ = viper.ReadInConfig() // missing file is fine

	return Settings{
		Profile:    viper.GetString("profile"),
		Region:     viper.GetString("region"),
		VPCID:      viper.GetString("vpc-id"),
		ServiceTag: viper.GetString("service-tag"),
		Verbose:    viper.GetBool("verbose"),
		ConfigFile: viper.ConfigFileUsed(),
	}
}
