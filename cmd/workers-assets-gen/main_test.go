package main

import (
	"reflect"
	"testing"
)

func TestParseClassNames(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []string
		wantErr bool
	}{
		{name: "empty", in: "", want: nil},
		{name: "single", in: "Counter", want: []string{"Counter"}},
		{name: "multiple, stray whitespace/commas", in: "Counter, ,Room", want: []string{"Counter", "Room"}},
		{name: "duplicate", in: "Counter,Counter", wantErr: true},
		{name: "duplicate after trimming", in: "Counter, Counter ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseClassNames(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseClassNames(%q) = %v, <nil>, want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseClassNames(%q) failed: %v", tt.in, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseClassNames(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseEntrypoints(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []entrypointSpec
		wantErr bool
	}{
		{name: "empty", in: "", want: nil},
		{
			name: "name only, no methods",
			in:   "Other",
			want: []entrypointSpec{{Name: "Other", Methods: nil}},
		},
		{
			name: "name with methods, mixing ; and , as intended",
			in:   "MyService:add,greet;Other",
			want: []entrypointSpec{
				{Name: "MyService", Methods: []string{"add", "greet"}},
				{Name: "Other", Methods: nil},
			},
		},
		{
			name: "stray whitespace and empty method dropped",
			in:   " MyService : add , ,greet ",
			want: []entrypointSpec{{Name: "MyService", Methods: []string{"add", "greet"}}},
		},
		{name: "empty class name", in: ":add", wantErr: true},
		{name: "duplicate class name", in: "MyService:add;MyService:greet", wantErr: true},
		{name: "duplicate method name within a spec", in: "MyService:add,add", wantErr: true},
		{name: "explicit fetch method name", in: "MyService:fetch", wantErr: true},
		{name: "explicit fetch method name among others", in: "MyService:add,fetch", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEntrypoints(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseEntrypoints(%q) = %+v, <nil>, want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEntrypoints(%q) failed: %v", tt.in, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseEntrypoints(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateClassNames(t *testing.T) {
	tests := []struct {
		name           string
		durableObjects []string
		workflows      []string
		entrypoints    []entrypointSpec
		wantErr        bool
	}{
		{
			name:           "disjoint names ok",
			durableObjects: []string{"Counter"},
			workflows:      []string{"MyWorkflow"},
			entrypoints:    []entrypointSpec{{Name: "MyService"}},
		},
		{
			name:           "durable object and workflow share a name",
			durableObjects: []string{"Foo"},
			workflows:      []string{"Foo"},
			wantErr:        true,
		},
		{
			name:        "workflow and entrypoint share a name",
			workflows:   []string{"Foo"},
			entrypoints: []entrypointSpec{{Name: "Foo"}},
			wantErr:     true,
		},
		{
			name:           "durable object and entrypoint share a name",
			durableObjects: []string{"Foo"},
			entrypoints:    []entrypointSpec{{Name: "Foo"}},
			wantErr:        true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateClassNames(tt.durableObjects, tt.workflows, tt.entrypoints)
			if tt.wantErr && err == nil {
				t.Fatal("validateClassNames() = <nil>, want an error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateClassNames() failed: %v", err)
			}
		})
	}
}
