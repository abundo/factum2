package templates

import _ "embed"

//go:embed eos_eline.tmpl
var EOSEline string

//go:embed iosxr_eline.tmpl
var IOSXREline string

//go:embed sros_eline.tmpl
var SROSEline string
