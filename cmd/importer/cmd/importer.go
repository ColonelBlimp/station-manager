package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/models"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/utils"
	"github.com/aarondl/sqlboiler/v4/boil"
)

func importer(filename string, logbookId int64) error {
	const op errors.Op = "cmd.importer"

	wdObj, err := container.ResolveSafe("workingdir")
	if err != nil {
		return errors.New(op).Err(err)
	}

	workDir, ok := wdObj.(string)
	if !ok {
		return errors.New(op).Msg("failed to cast working directory to string")
	}

	fpath, err := checkAdiFile(workDir, filename)
	if err != nil {
		return errors.New(op).Err(err)
	}

	data, err := readFile(fpath)
	if err != nil {
		return errors.New(op).Err(err)
	}

	adiObj, err := adif.Marshal(data)
	if err != nil {
		return errors.New(op).Err(err)
	}

	if err = insertIntoDatabase(adiObj, logbookId); err != nil {
		return errors.New(op).Err(err)
	}

	return nil
}

func checkAdiFile(workingDir, filename string) (string, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return "", fmt.Errorf("filename cannot be empty")
	}

	fpath := filepath.Join(workingDir, filename)
	exists, err := utils.PathExists(fpath)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("ADI file not found at %s", fpath)
	}

	fmt.Printf("Importing file: %s\n", fpath)

	return fpath, nil
}

func readFile(filePath string) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	var data []byte
	if data, err = io.ReadAll(file); err != nil {
		return nil, err
	}

	return data, nil
}

func insertIntoDatabase(adiObj adif.Adif, logbookId int64) error {
	const op errors.Op = "cmd.insertIntoDatabase"
	if err := db.Open(); err != nil {
		return errors.New(op).Err(err).Msg("failed to open database connection")
	}

	sessionId, err := db.GenerateSession()
	if err != nil {
		return errors.New(op).Err(err).Msg("failed to generate session ID")
	}

	// We assume each record takes 200ms to insert into the database
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(len(adiObj.Records))*(200*time.Millisecond))
	defer cancel()

	tx, cancelCtx, err := db.BeginTxContext(ctx)
	if err != nil {
		return errors.New(op).Err(err).Msg("failed to begin transaction")
	}
	defer cancelCtx()
	defer func(tx *sql.Tx) {
		_ = tx.Rollback()
	}(tx)

	var model *models.Qso
	count := 0
	for _, record := range adiObj.Records {
		if model, err = adiRecordToQsoModel(&record, logbookId, sessionId); err != nil {
			return errors.New(op).Err(err).Msg("failed to convert ADI record to QSO model")
		}

		if err = model.Insert(ctx, tx, boil.Infer()); err != nil {
			return errors.New(op).Err(err).Msg("failed to insert QSO model into database")
		}
		count++

		//TODO: Add country to country table. Populate from the hamnut.com
	}

	if err = tx.Commit(); err != nil {
		return errors.New(op).Err(err).Msg("failed to commit transaction")
	}

	fmt.Printf("Inserted %d records into database\n", count)

	return nil
}

func adiRecordToQsoModel(r *adif.Record, logbookId, sessionId int64) (*models.Qso, error) {
	const op errors.Op = "cmd.adiRecordToQsoModel"
	c := *r

	// blank the fields NOT to be stored in the additionalData field
	c.Call = ""
	c.Band = ""
	c.Mode = ""
	c.QsoDate = ""
	c.TimeOn = ""
	c.TimeOff = ""
	c.RstSent = ""
	c.RstRcvd = ""
	c.Country = ""
	c.Freq = ""

	data, err := json.Marshal(c)
	if err != nil {
		return nil, errors.New(op).Err(err).Msg("failed to marshal ADI record to JSON")
	}

	freqMHz, err := strconv.ParseFloat(r.Freq, 64)
	if err != nil {
		return nil, errors.New(op).Err(err).Msg("invalid frequency value")
	}
	freqHz := int64(math.Round(freqMHz * 1_000_000))
	rstSent := strings.TrimSpace(r.RstSent)
	if len(rstSent) > 3 {
		rstSent = rstSent[:3]
	}
	rstRcvd := strings.TrimSpace(r.RstRcvd)
	if len(rstRcvd) > 3 {
		rstRcvd = rstRcvd[:3]
	}

	model := &models.Qso{
		Call:           r.Call,
		Band:           r.Band,
		Mode:           r.Mode,
		Freq:           freqHz,
		QsoDate:        r.QsoDate,
		TimeOn:         r.TimeOn,
		TimeOff:        r.TimeOff,
		RstSent:        rstSent,
		RstRcvd:        rstRcvd,
		Country:        r.Country,
		AdditionalData: data,
		LogbookID:      logbookId,
		SessionID:      sessionId,
	}
	return model, nil
}
