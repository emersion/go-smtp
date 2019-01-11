package backendutil_test

import (
	"github.com/emersion/go-smtp"
	"github.com/emersion/go-smtp/backendutil"
)

var _ smtp.Backend = &backendutil.TransformBackend{}
