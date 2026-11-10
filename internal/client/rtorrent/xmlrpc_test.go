package rtorrent

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestEncodeCall(t *testing.T) {
	b, err := encodeCall("load.raw", []any{"", Base64("d1:ae"), "d.custom1.set=a&b", 5, int64(1) << 40, []any{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"<methodName>load.raw</methodName>",
		"<value><string></string></value>",
		"<value><base64>ZDE6YWU=</base64></value>",
		"<value><string>d.custom1.set=a&amp;b</string></value>",
		"<value><i4>5</i4></value>",
		"<value><i8>1099511627776</i8></value>",
		"<value><array><data><value><string>x</string></value></data></array></value>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
}

func TestDecodeResponse(t *testing.T) {
	v, err := decodeResponse([]byte(`<?xml version="1.0"?><methodResponse><params><param><value><array><data>
		<value><array><data><value><string>ABC</string></value><value><i8>5000000000</i8></value><value>plain</value><value><string></string></value></data></array></value>
	</data></array></value></param></params></methodResponse>`))
	if err != nil {
		t.Fatal(err)
	}
	want := []any{[]any{"ABC", int64(5000000000), "plain", ""}}
	if !reflect.DeepEqual(v, want) {
		t.Errorf("got %#v", v)
	}

	_, err = decodeResponse([]byte(`<?xml version="1.0"?><methodResponse><fault><value><struct>
		<member><name>faultCode</name><value><i4>-501</i4></value></member>
		<member><name>faultString</name><value><string>Could not find info-hash.</string></value></member>
	</struct></value></fault></methodResponse>`))
	var f *Fault
	if !errors.As(err, &f) || f.Code != -501 || f.Message != "Could not find info-hash." {
		t.Errorf("fault = %v", err)
	}
	if _, err := decodeResponse([]byte("<html>login</html>")); !errors.Is(err, errNotXMLRPC) {
		t.Errorf("html should not decode, got %v", err)
	}
}
