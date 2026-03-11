package cli

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// Root is the top-level cobra command.
var Root = &cobra.Command{
	Use:   "mxhook",
	Short: "MXHook — inbound email processing platform",
	Long:  "MXHook receives inbound email via SMTP, parses it, and delivers structured webhooks.",
}

func init() {
	cobra.OnInitialize(initConfig)
	Root.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: mxhook.yaml)")
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("mxhook")
		viper.AddConfigPath(".")
		viper.AddConfigPath("/etc/mxhook/")
	}
	viper.SetEnvPrefix("MXHOOK")
	viper.AutomaticEnv()
	_ = viper.ReadInConfig()
}
