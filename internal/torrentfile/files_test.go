package torrentfile

import (
	"reflect"
	"strings"
	"testing"
)

func layoutOf(t *testing.T, info string) (Layout, error) {
	t.Helper()
	f, err := Parse([]byte("d4:info" + info + "e"))
	if err != nil {
		t.Fatal(err)
	}
	return f.Layout()
}

func TestLayoutSingle(t *testing.T) {
	l, err := layoutOf(t, "d6:lengthi5e4:name5:a.mkv12:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaae")
	if err != nil {
		t.Fatal(err)
	}
	if l.Multi || l.Name != "a.mkv" || l.Total() != 5 || !reflect.DeepEqual(l.RelPaths(), []string{"a.mkv"}) {
		t.Errorf("got %+v %v", l, l.RelPaths())
	}
}

func TestLayoutMultiSkipsPadding(t *testing.T) {
	info := "d5:filesl" +
		"d6:lengthi3e4:pathl7:E01.mkveed" +
		"4:attr1:p6:lengthi9e4:pathl4:.pad1:9eed" +
		"6:lengthi4e4:pathl4:Subs7:E01.srteee" +
		"4:name4:Pack12:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaae"
	l, err := layoutOf(t, info)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Pack/E01.mkv", "Pack/Subs/E01.srt"}
	if !l.Multi || l.Total() != 7 || !reflect.DeepEqual(l.RelPaths(), want) {
		t.Errorf("got %+v %v", l, l.RelPaths())
	}
}

func TestLayoutV2Tree(t *testing.T) {
	info := "d9:file treed" +
		"7:E01.mkvd0:d6:lengthi3eee" +
		"4:Subsd7:E01.srtd0:d6:lengthi4eeee" +
		"e4:name4:Pack12:meta versioni2e12:piece lengthi16384ee"
	l, err := layoutOf(t, info)
	if err != nil {
		t.Fatal(err)
	}
	if !l.Multi || !reflect.DeepEqual(l.RelPaths(), []string{"Pack/E01.mkv", "Pack/Subs/E01.srt"}) {
		t.Errorf("got %+v %v", l, l.RelPaths())
	}
	single, err := layoutOf(t, "d9:file treed5:a.mkvd0:d6:lengthi7eeee4:name5:a.mkv12:meta versioni2ee")
	if err != nil || single.Multi || single.Total() != 7 {
		t.Errorf("single v2: %+v %v", single, err)
	}
}

func TestLayoutRejectsEscapes(t *testing.T) {
	for _, info := range []string{
		"d6:lengthi5e4:name2:..e",
		"d6:lengthi5e4:name0:e",
		"d5:filesld6:lengthi3e4:pathl2:..6:passwdeee4:name4:Packe",
		"d5:filesld6:lengthi3e4:pathl5:a/b/ceee4:name4:Packe",
	} {
		if _, err := layoutOf(t, info); err == nil || !strings.Contains(err.Error(), "unsafe") && !strings.Contains(err.Error(), "invalid") {
			t.Errorf("%s: want rejection, got %v", info, err)
		}
	}
}
