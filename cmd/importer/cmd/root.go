package cmd

import (
	"reflect"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/utils"
	"github.com/spf13/cobra"
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import ADIF files",
	Long:  "Import ADIF files into the Station Manager database.",
	RunE: func(cmd *cobra.Command, args []string) error {
		logbookId, err := cmd.Flags().GetInt64("logbook_id")
		if err != nil {
			return err
		}
		file, err := cmd.Flags().GetString("file")
		if err != nil {
			return err
		}
		if err = importer(file, logbookId); err != nil {
			return err
		}
		return nil
	},
}

// var workingDir string
var container *iocdi.Container
var db *sqlite.Service

func Execute() {
	cobra.CheckErr(importCmd.Execute())
}

func init() {
	workingDir, err := utils.WorkingDir()
	if err != nil {
		panic(err)
	}

	container = iocdi.New()
	err = container.RegisterInstance("workingdir", workingDir)
	cobra.CheckErr(err)
	err = container.Register(config.ServiceName, reflect.TypeOf((*config.Service)(nil)))
	cobra.CheckErr(err)
	err = container.Register(sqlite.ServiceName, reflect.TypeOf((*sqlite.Service)(nil)))
	cobra.CheckErr(err)
	err = container.Register(logging.ServiceName, reflect.TypeOf((*logging.Service)(nil)))
	cobra.CheckErr(err)

	err = container.Build()
	cobra.CheckErr(err)

	//cfgInstance, err := container.ResolveSafe(config.ServiceName)
	//cobra.CheckErr(err)
	var ok bool
	//cfg, ok = cfgInstance.(*config.Service)
	//if !ok {
	//	panic("cfg is not of type *config.Service")
	//}
	dbInstance, err := container.ResolveSafe(sqlite.ServiceName)
	cobra.CheckErr(err)
	db, ok = dbInstance.(*sqlite.Service)
	if !ok {
		panic("db is not of type *sqlite.Service")
	}

	importCmd.Flags().StringP("file", "f", "", "ADIF file to import")
	importCmd.Flags().Int64P("logbook_id", "l", 0, "Logbook ID to import into")
	cobra.CheckErr(importCmd.MarkFlagRequired("file"))
	cobra.CheckErr(importCmd.MarkFlagRequired("logbook_id"))
}
