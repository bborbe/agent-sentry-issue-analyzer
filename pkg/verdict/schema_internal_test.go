// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package verdict

import (
	"reflect"
	"strings"
	"testing"
)

// TestFieldsMatchSchema locks verdictFields against the Verdict struct's yaml
// tags. quoteProseValues uses that list to decide where a folded prose value
// ends, so a field added to the struct without landing here would let a wrapped
// prose line that begins with the new key silently truncate the value - the
// same silent-truncation failure the fold exists to prevent.
func TestFieldsMatchSchema(t *testing.T) {
	typ := reflect.TypeOf(Verdict{})
	if typ.NumField() != len(verdictFields) {
		t.Fatalf(
			"verdictFields has %d keys, Verdict has %d fields",
			len(verdictFields),
			typ.NumField(),
		)
	}
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.Split(typ.Field(i).Tag.Get("yaml"), ",")[0]
		if tag != verdictFields[i] {
			t.Errorf("field %d: struct tag %q != verdictFields %q", i, tag, verdictFields[i])
		}
	}
	for _, f := range proseFields {
		if !contains(verdictFields, f) {
			t.Errorf("prose field %q missing from verdictFields", f)
		}
	}
}
