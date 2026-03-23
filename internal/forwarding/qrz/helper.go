package qrz

import (
	"html"
	"net/url"

	"github.com/Station-Manager/errors"
)

func parseResponse(body []byte) (*Response, error) {
	const op errors.Op = "forwarder.qrz.parseResponse"
	decoded, err := url.QueryUnescape(string(body))
	if err != nil {
		return nil, errors.New(op).Err(err).Msg("url.QueryUnescape") //fmt.Errorf("parseResponse: %w", err)
	}
	str := html.UnescapeString(decoded)
	values, err := url.ParseQuery(str)
	if err != nil {
		return nil, errors.New(op).Err(err).Msg("url.ParseQuery") //fmt.Errorf("parseResponse: %w", err)
	}

	resp := &Response{}

	for k, v := range values {
		switch k {
		case "RESULT":
			resp.Result = v[0]
		case "REASON":
			resp.Reason = v[0]
		case "LOGIDS":
			resp.LogIDS = v[0]
		case "LOGID":
			resp.LogID = v[0]
		case "COUNT":
			resp.Count = v[0]
		case "DATA":
			resp.Data = v[0]
		}
	}

	return resp, nil
}
