package cmd

import (
	"github.com/spf13/cobra"
)

var RootCmd = &cobra.Command{
	Use:   "vidsplash",
	Short: "A small video-editing toolkit built on ffmpeg",
}

func init() {
	registerPersistentFlags(RootCmd)
	RootCmd.AddCommand(splashCmd)
	RootCmd.AddCommand(concatCmd)
	RootCmd.AddCommand(splitCmd)
	RootCmd.AddCommand(captionCmd)
}
