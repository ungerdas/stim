package rtorrent

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Just enough XML-RPC for rTorrent: the value types it sends and accepts.

// Fault is an XML-RPC fault answer.
type Fault struct {
	Code    int
	Message string
}

func (f *Fault) Error() string { return fmt.Sprintf("rTorrent: %s (%d)", f.Message, f.Code) }

// Base64 marks a parameter to be sent as <base64>.
type Base64 []byte

func encodeValue(b *bytes.Buffer, v any) error {
	b.WriteString("<value>")
	switch x := v.(type) {
	case string:
		b.WriteString("<string>")
		if err := xml.EscapeText(b, []byte(x)); err != nil {
			return err
		}
		b.WriteString("</string>")
	case int:
		writeInt(b, int64(x))
	case int64:
		writeInt(b, x)
	case bool:
		if x {
			b.WriteString("<boolean>1</boolean>")
		} else {
			b.WriteString("<boolean>0</boolean>")
		}
	case Base64:
		b.WriteString("<base64>")
		b.WriteString(base64.StdEncoding.EncodeToString(x))
		b.WriteString("</base64>")
	case []any:
		b.WriteString("<array><data>")
		for _, e := range x {
			if err := encodeValue(b, e); err != nil {
				return err
			}
		}
		b.WriteString("</data></array>")
	case []string:
		b.WriteString("<array><data>")
		for _, e := range x {
			if err := encodeValue(b, e); err != nil {
				return err
			}
		}
		b.WriteString("</data></array>")
	case map[string]any:
		b.WriteString("<struct>")
		for k, e := range x {
			b.WriteString("<member><name>")
			if err := xml.EscapeText(b, []byte(k)); err != nil {
				return err
			}
			b.WriteString("</name>")
			if err := encodeValue(b, e); err != nil {
				return err
			}
			b.WriteString("</member>")
		}
		b.WriteString("</struct>")
	default:
		return fmt.Errorf("xmlrpc: cannot encode %T", v)
	}
	b.WriteString("</value>")
	return nil
}

func writeInt(b *bytes.Buffer, n int64) {
	if n >= -1<<31 && n < 1<<31 {
		b.WriteString("<i4>" + strconv.FormatInt(n, 10) + "</i4>")
	} else {
		b.WriteString("<i8>" + strconv.FormatInt(n, 10) + "</i8>")
	}
}

func encodeCall(method string, params []any) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0"?><methodCall><methodName>`)
	if err := xml.EscapeText(&b, []byte(method)); err != nil {
		return nil, err
	}
	b.WriteString("</methodName><params>")
	for _, p := range params {
		b.WriteString("<param>")
		if err := encodeValue(&b, p); err != nil {
			return nil, err
		}
		b.WriteString("</param>")
	}
	b.WriteString("</params></methodCall>")
	return b.Bytes(), nil
}

type xValue struct {
	String *string `xml:"string"`
	I4     *string `xml:"i4"`
	I8     *string `xml:"i8"`
	Int    *string `xml:"int"`
	Bool   *string `xml:"boolean"`
	Double *string `xml:"double"`
	Base64 *string `xml:"base64"`
	Array  *struct {
		Values []xValue `xml:"data>value"`
	} `xml:"array"`
	Struct *struct {
		Members []struct {
			Name  string `xml:"name"`
			Value xValue `xml:"value"`
		} `xml:"member"`
	} `xml:"struct"`
	Text string `xml:",chardata"`
}

func (v xValue) decode() (any, error) {
	switch {
	case v.String != nil:
		return *v.String, nil
	case v.I4 != nil, v.I8 != nil, v.Int != nil:
		s := v.I4
		if s == nil {
			s = v.I8
		}
		if s == nil {
			s = v.Int
		}
		return strconv.ParseInt(strings.TrimSpace(*s), 10, 64)
	case v.Bool != nil:
		return strings.TrimSpace(*v.Bool) == "1", nil
	case v.Double != nil:
		return strconv.ParseFloat(strings.TrimSpace(*v.Double), 64)
	case v.Base64 != nil:
		return base64.StdEncoding.DecodeString(strings.TrimSpace(*v.Base64))
	case v.Array != nil:
		out := make([]any, 0, len(v.Array.Values))
		for _, e := range v.Array.Values {
			d, err := e.decode()
			if err != nil {
				return nil, err
			}
			out = append(out, d)
		}
		return out, nil
	case v.Struct != nil:
		out := make(map[string]any, len(v.Struct.Members))
		for _, m := range v.Struct.Members {
			d, err := m.Value.decode()
			if err != nil {
				return nil, err
			}
			out[m.Name] = d
		}
		return out, nil
	default:
		return v.Text, nil // a value without a type is a string
	}
}

type xResponse struct {
	Params []struct {
		Value xValue `xml:"value"`
	} `xml:"params>param"`
	Fault *struct {
		Value xValue `xml:"value"`
	} `xml:"fault"`
}

var errNotXMLRPC = errors.New("not an XML-RPC answer")

func decodeResponse(body []byte) (any, error) {
	var r xResponse
	if err := xml.Unmarshal(body, &r); err != nil {
		return nil, errNotXMLRPC
	}
	if r.Fault != nil {
		v, err := r.Fault.Value.decode()
		if err != nil {
			return nil, errNotXMLRPC
		}
		m, _ := v.(map[string]any)
		code, _ := m["faultCode"].(int64)
		msg, _ := m["faultString"].(string)
		return nil, &Fault{Code: int(code), Message: msg}
	}
	if len(r.Params) == 0 {
		return nil, errNotXMLRPC
	}
	return r.Params[0].Value.decode()
}

// Helpers for reading decoded values.

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case []byte:
		return string(x)
	}
	return ""
}

func asInt(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	case bool:
		if x {
			return 1
		}
	case float64:
		return int64(x)
	}
	return 0
}

func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimSpace(s))
}
