package server

import (
 "bytes"
 "io"
 "log"
 "net/http"
 "strings"
 "testing"
)

func TestEmailRejectionDiagnostics(t *testing.T) {
 var output bytes.Buffer
 previous := log.Writer()
 log.SetOutput(&output)
 defer log.SetOutput(previous)
 cases := []struct {
  body string
  status int
  want string
 }{
  {`{"code":"unauthorized","message":"We have detected you are using an unrecognized IP address secret@example.com"}`, 401, "reason=ip_not_authorized"},
  {`{"code":"invalid_parameter","message":"sender secret@example.com is not valid"}`, 400, "reason=sender_not_authorized"},
  {`{"code":"not_enough_credits","message":"quota exceeded secret@example.com"}`, 402, "reason=quota_or_credits"},
  {`{"code":"unauthorized","message":"secret@example.com"}`, 401, "reason=authentication_failed"},
  {`{"code":"secret@example.com","message":"token=private"}`, 429, "reason=rate_limited"},
  {"not JSON secret@example.com", 500, "reason=provider_rejected"},
 }
 for _, c := range cases {
  output.Reset()
  response := &http.Response{StatusCode:c.status, Body:io.NopCloser(strings.NewReader(c.body))}
  logEmailRejection("brevo", response)
  got := output.String()
  if !strings.Contains(got, c.want) {
   t.Fatalf("missing classification %q: %s", c.want, got)
  }
  for _, sensitive := range []string{"secret@example.com", "token=private", "We have detected"} {
   if strings.Contains(got, sensitive) {
    t.Fatalf("diagnostics leaked provider data: %s", got)
   }
  }
 }
}
