package errno

import (
	"testing"
)

func TestError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "Standard Error",
			err:  New(10001, "internal server error"),
			want: "code: 10001, message: internal server error",
		},
		{
			name: "Success Code",
			err:  New(0, "success"),
			want: "code: 0, message: success",
		},
		{
			name: "Custom Error",
			err:  New(20001, "task not found"),
			want: "code: 20001, message: task not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	err := New(10002, "invalid parameter")

	if err.Code != 10002 {
		t.Errorf("New() Code = %d, want 10002", err.Code)
	}

	if err.Message != "invalid parameter" {
		t.Errorf("New() Message = %s, want 'invalid parameter'", err.Message)
	}
}

func TestPredefinedErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     *Error
		wantCode int32
		wantMsg string
	}{
		{
			name:    "OK",
			err:     OK,
			wantCode: 0,
			wantMsg: "success",
		},
		{
			name:    "ErrInternalServerError",
			err:     ErrInternalServerError,
			wantCode: 10001,
			wantMsg: "internal server error",
		},
		{
			name:    "ErrInvalidParam",
			err:     ErrInvalidParam,
			wantCode: 10002,
			wantMsg: "invalid parameter",
		},
		{
			name:    "ErrTaskNotFound",
			err:     ErrTaskNotFound,
			wantCode: 20001,
			wantMsg: "task not found",
		},
		{
			name:    "ErrTaskAlreadyExist",
			err:     ErrTaskAlreadyExist,
			wantCode: 20002,
			wantMsg: "task already exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.wantCode {
				t.Errorf("%s.Code = %d, want %d", tt.name, tt.err.Code, tt.wantCode)
			}
			if tt.err.Message != tt.wantMsg {
				t.Errorf("%s.Message = %s, want %s", tt.name, tt.err.Message, tt.wantMsg)
			}
		})
	}
}

func TestError_AsStandardError(t *testing.T) {
	// 验证 *Error 实现了 error 接口
	var _ error = (*Error)(nil)
	var _ error = New(10001, "test")
	
	// 可以作为标准 error 使用
	err := New(10002, "test error")
	errorMsg := err.Error()
	
	if errorMsg == "" {
		t.Error("Error() should return non-empty string")
	}
}
