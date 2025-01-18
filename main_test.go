package main

import "testing"

func TestConvertStringToInt(t *testing.T) {
	type args struct {
		n string
	}
	tests := []struct {
		name    string
		args    args
		want    int
		wantErr bool
	}{
		// TODO: Add test cases.
		{"Valid number", args{n: "123"}, 123, false},
		{"Zero", args{n: "0"}, 0, false},
		{"Negative number", args{n: "-456"}, -456, false},
		{"Invalid string", args{n: "abc"}, 0, true}, // Should return an error
		{"Empty string", args{n: ""}, 0, true},      // Should return an error
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertStringToInt(tt.args.n)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertStringToInt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ConvertStringToInt() got = %v, want %v", got, tt.want)
			}
		})
	}
}
