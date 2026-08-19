package webui

import (
	"fmt"
	"strings"
	"testing"
)

func TestLocaleEntries_whenRegistered_haveBothTranslations(t *testing.T) {
	for key, entry := range localeEntries {
		if entry[0] == "" {
			t.Errorf("locale entry %q has empty zh translation", key)
		}
		if entry[1] == "" {
			t.Errorf("locale entry %q has empty en translation", key)
		}
	}
}

func TestTranslate_whenLanguageVaries_selectsExpectedTranslation(t *testing.T) {
	tests := []struct {
		name string
		lang string
		want string
	}{
		{name: "empty language uses Chinese", lang: "", want: "保存"},
		{name: "unknown language uses Chinese", lang: "xx", want: "保存"},
		{name: "English language uses English", lang: "en", want: "Save"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translate(tt.lang, "common.save")
			if got != tt.want {
				t.Fatalf("translate(%q, common.save) = %q, want %q", tt.lang, got, tt.want)
			}
		})
	}
}

func TestTranslate_whenKeyIsUnknown_returnsKey(t *testing.T) {
	const key = "missing.translation"

	got := translate("en", key)

	if got != key {
		t.Fatalf("translate(en, %q) = %q, want key unchanged", key, got)
	}
}

func TestTranslate_whenArgumentsProvided_formatsTranslation(t *testing.T) {
	got := translate("zh", "files.delete_confirm", "notes/today.md")
	want := "确定删除 notes/today.md？删除后可在回收站中恢复。"

	if got != want {
		t.Fatalf("formatted translation = %q, want %q", got, want)
	}
}

func TestRegisterEntries_whenKeyIsDuplicated_panicsWithKey(t *testing.T) {
	const key = "test.duplicate"
	entries := map[string][2]string{key: {"测试", "Test"}}
	defer delete(localeEntries, key)

	registerEntries(entries)

	defer func() {
		value := recover()
		if value == nil {
			t.Fatal("registerEntries did not panic for a duplicate key")
		}
		if !strings.Contains(fmt.Sprint(value), key) {
			t.Fatalf("duplicate panic %q does not include key %q", value, key)
		}
	}()
	registerEntries(entries)
}

func TestLanguages_whenListed_returnsSupportedOrder(t *testing.T) {
	got := Languages()
	want := []string{"zh", "en"}

	if len(got) != len(want) {
		t.Fatalf("Languages() = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("Languages() = %v, want %v", got, want)
		}
	}
}
