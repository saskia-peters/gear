package core

import (
	"errors"
	"testing"
)

func TestRegisterInputValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   RegisterInput
		wantErr error
	}{
		{
			name: "valid input",
			input: RegisterInput{
				FirstName:       "Max",
				LastName:        "Mustermann",
				Email:           "max@example.com",
				Password:        "1234567890",
				PasswordConfirm: "1234567890",
			},
			wantErr: nil,
		},
		{
			name: "missing first name",
			input: RegisterInput{
				FirstName:       "  ",
				LastName:        "Mustermann",
				Email:           "max@example.com",
				Password:        "1234567890",
				PasswordConfirm: "1234567890",
			},
			wantErr: ErrMissingFields,
		},
		{
			name: "missing last name",
			input: RegisterInput{
				FirstName:       "Max",
				LastName:        "",
				Email:           "max@example.com",
				Password:        "1234567890",
				PasswordConfirm: "1234567890",
			},
			wantErr: ErrMissingFields,
		},
		{
			name: "missing email",
			input: RegisterInput{
				FirstName:       "Max",
				LastName:        "Mustermann",
				Email:           "",
				Password:        "1234567890",
				PasswordConfirm: "1234567890",
			},
			wantErr: ErrMissingFields,
		},
		{
			name: "invalid email format",
			input: RegisterInput{
				FirstName:       "Max",
				LastName:        "Mustermann",
				Email:           "invalid-email",
				Password:        "1234567890",
				PasswordConfirm: "1234567890",
			},
			wantErr: ErrInvalidEmail,
		},
		{
			name: "short password < 10 chars",
			input: RegisterInput{
				FirstName:       "Max",
				LastName:        "Mustermann",
				Email:           "max@example.com",
				Password:        "short9ch",
				PasswordConfirm: "short9ch",
			},
			wantErr: ErrShortPassword,
		},
		{
			name: "password mismatch",
			input: RegisterInput{
				FirstName:       "Max",
				LastName:        "Mustermann",
				Email:           "max@example.com",
				Password:        "1234567890",
				PasswordConfirm: "0987654321",
			},
			wantErr: ErrPasswordMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
