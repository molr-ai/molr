package app

import "molr.ai/runtime/internal/textfiles"

var openResourceFile = textfiles.Open
var openResourceDirectory = textfiles.OpenDirectory
var resourceFileVersion = textfiles.Version
var replaceResourceFile = textfiles.Replace
var createOutputFile = textfiles.CreateOutput
var appendOutputChunk = textfiles.AppendOutput
var fileChanged = textfiles.Changed
