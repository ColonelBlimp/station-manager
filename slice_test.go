//go:build integration

package adif

import (
	"github.com/Station-Manager/adapters"
	"github.com/Station-Manager/config"
	"github.com/Station-Manager/database/sqlite"
	models "github.com/Station-Manager/database/sqlite/models"
	"github.com/Station-Manager/logging"
	"github.com/Station-Manager/types"
	"github.com/Station-Manager/utils"
	"golang.org/x/net/context"
	"html"
	"testing"
)

func TestComposeToAdifString(t *testing.T) {
	t.Skip("integration test skipped in unit test run")
	cfg, err := config.New()
	if err != nil {
		t.Fatalf("Failed to create config: %v", err)
		return
	}

	log, err := logging.NewLogger()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
		return
	}
	log.Debugf("here: %s", cfg.WorkingDir())

	var dbConf types.DataStoreConfig
	if err = cfg.Get().Unmarshal(config.DatastoreSqliteKey, &dbConf); err != nil {
		t.Fatal(err)
		return
	}

	db, err := sqlite.New(cfg.WorkingDir(), &dbConf, log)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
		return
	}
	slice, err := models.Qsos().All(context.Background(), db.Exec())
	if err != nil {
		t.Fatalf("Failed to load QSOs: %v", err)
		return
	}

	list, err := qsoModelSliceToQsoTypeSlice(slice)
	if err != nil {
		t.Fatalf("Failed to convert QSO model slice to QSO type slice: %v", err)
		return
	}

	t.Log(len(list))

	_, err = ComposeToAdifString(list)
	if err != nil {
		t.Fatalf("Failed to compose ADIF string: %v", err)
		return
	}

	//	t.Log(adifStr)
}

func qsoModelSliceToQsoTypeSlice(slice models.QsoSlice) (types.QsoSlice, error) {
	var qsoList []types.Qso
	for _, model := range slice {
		item, err := adapters.ConvertModelToType[types.Qso](model)
		if err != nil {
			return nil, err
		}

		//		item.Name = html.EscapeString(item.Name)
		if item.Name, err = utils.DecodeStringToUTF8(item.Name); err != nil {
			item.Name = html.EscapeString(item.Name)
		}
		if len(item.Name) > 100 {
			item.Name = item.Name[:100]
		}

		item.QsoDate = utils.FormatDate(item.QsoDate)
		item.QsoDateOff = utils.FormatDate(item.QsoDateOff)
		item.TimeOn = utils.FormatTime(item.TimeOn)
		item.TimeOff = utils.FormatTime(item.TimeOff)

		qsoList = append(qsoList, *item)
	}

	return qsoList, nil
}
