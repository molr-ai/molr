package app

import (
	"crypto/sha256"
	"encoding/hex"
	"molr.ai/runtime/internal/textfiles"
)

var openResourceFile = textfiles.Open
var openResourceDirectory = textfiles.OpenDirectory
var resourceFileVersion = textfiles.Version
var replaceResourceFile = textfiles.Replace
var createOutputFile = textfiles.CreateOutput
var appendOutputChunk = textfiles.AppendOutput
var fileChanged = textfiles.Changed

func digestBytes(b []byte) string { d := sha256.Sum256(b); return hex.EncodeToString(d[:]) }
