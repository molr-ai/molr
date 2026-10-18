package installationwindows

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

const taskNamespace = "http://schemas.microsoft.com/windows/2004/02/mit/task"

type taskPrincipal struct {
	ID       string `xml:"id,attr"`
	UserID   string `xml:"UserId"`
	GroupID  string `xml:"GroupId"`
	Logon    string `xml:"LogonType"`
	RunLevel string
}
type taskExec struct{ Command, Arguments, WorkingDirectory string }
type taskActions struct {
	Context string `xml:"Context,attr"`
	Exec    []taskExec
	Other   []struct{ XMLName xml.Name } `xml:",any"`
}
type taskDefinition struct {
	XMLName    xml.Name        `xml:"Task"`
	Principals []taskPrincipal `xml:"Principals>Principal"`
	Actions    []taskActions
}

// /Query output can be one Task or a Tasks document containing it. Never decode
// an unknown wrapper into zero-value identity fields, and never select the first
// item from a multi-task query when deciding which task may be changed.
func parseTaskXML(raw []byte) (taskDefinition, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var start xml.StartElement
	for {
		token, err := decoder.Token()
		if err != nil {
			return taskDefinition{}, fmt.Errorf("scheduled task XML: %w", err)
		}
		if element, ok := token.(xml.StartElement); ok {
			start = element
			break
		}
	}
	if start.Name.Space != "" && start.Name.Space != taskNamespace {
		return taskDefinition{}, fmt.Errorf("scheduled task XML: unexpected namespace %q", start.Name.Space)
	}
	var task taskDefinition
	switch start.Name.Local {
	case "Task":
		if err := decoder.DecodeElement(&task, &start); err != nil {
			return task, fmt.Errorf("scheduled task XML: %w", err)
		}
	case "Tasks":
		var wrapper struct {
			Tasks []taskDefinition             `xml:"Task"`
			Other []struct{ XMLName xml.Name } `xml:",any"`
		}
		if err := decoder.DecodeElement(&wrapper, &start); err != nil {
			return task, fmt.Errorf("scheduled task XML: %w", err)
		}
		if len(wrapper.Tasks) != 1 || len(wrapper.Other) != 0 {
			return task, fmt.Errorf("scheduled task XML: expected one Task, got %d", len(wrapper.Tasks))
		}
		task = wrapper.Tasks[0]
	default:
		return task, fmt.Errorf("scheduled task XML: unsupported root %q", start.Name.Local)
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return task, fmt.Errorf("scheduled task XML: %w", err)
		}
		switch v := token.(type) {
		case xml.Comment:
		case xml.CharData:
			if strings.TrimSpace(string(v)) != "" {
				return task, fmt.Errorf("scheduled task XML: trailing content")
			}
		default:
			return task, fmt.Errorf("scheduled task XML: multiple root elements")
		}
	}
	if task.XMLName.Space != "" && task.XMLName.Space != taskNamespace {
		return task, fmt.Errorf("scheduled task XML: unexpected Task namespace")
	}
	if len(task.Principals) != 1 {
		return task, fmt.Errorf("scheduled task Principals: expected one Principal, got %d", len(task.Principals))
	}
	principal := &task.Principals[0]
	principal.UserID = strings.TrimSpace(principal.UserID)
	if principal.UserID == "" || principal.GroupID != "" {
		return task, fmt.Errorf("scheduled task Principal.UserId: an explicit user account is required")
	}
	// MS-TSCH section 3.2.5.4.2: omitted LogonType is InteractiveToken.
	if principal.Logon == "" {
		principal.Logon = "InteractiveToken"
	}
	if principal.Logon != "InteractiveToken" {
		return task, fmt.Errorf("scheduled task Principal.LogonType mismatch: expected InteractiveToken")
	}
	// Task Scheduler's schema defaults an omitted RunLevel to LeastPrivilege.
	if principal.RunLevel == "" {
		principal.RunLevel = "LeastPrivilege"
	}
	if principal.RunLevel != "LeastPrivilege" {
		return task, fmt.Errorf("scheduled task Principal.RunLevel mismatch: expected LeastPrivilege")
	}
	if len(task.Actions) != 1 || len(task.Actions[0].Exec) != 1 || len(task.Actions[0].Other) != 0 {
		return task, fmt.Errorf("scheduled task Actions: expected exactly one Exec action")
	}
	if task.Actions[0].Context != "" && task.Actions[0].Context != principal.ID {
		return task, fmt.Errorf("scheduled task Actions.Context does not reference its Principal")
	}
	execution := task.Actions[0].Exec[0]
	if strings.TrimSpace(execution.Command) == "" {
		return task, fmt.Errorf("scheduled task Exec.Command is missing")
	}
	if strings.TrimSpace(execution.Arguments) == "" {
		return task, fmt.Errorf("scheduled task Exec.Arguments is missing")
	}
	if strings.TrimSpace(execution.WorkingDirectory) == "" {
		return task, fmt.Errorf("scheduled task Exec.WorkingDirectory is missing")
	}
	return task, nil
}

// Compare the installed receipt's task, not the version being offered for
// upgrade. Default XML values are normalized by parseTaskXML; paths and command
// arguments retain the original exact ownership check.
func compareTaskIdentity(actualXML, expectedXML []byte) error {
	actual, err := parseTaskXML(actualXML)
	if err != nil {
		return err
	}
	expected, err := parseTaskXML(expectedXML)
	if err != nil {
		return fmt.Errorf("expected task definition: %w", err)
	}
	if actual.Principals[0].UserID != expected.Principals[0].UserID {
		return fmt.Errorf("scheduled task Principal.UserId mismatch")
	}
	a, b := actual.Actions[0].Exec[0], expected.Actions[0].Exec[0]
	for _, field := range []struct{ name, actual, expected string }{{"Command", a.Command, b.Command}, {"Arguments", a.Arguments, b.Arguments}, {"WorkingDirectory", a.WorkingDirectory, b.WorkingDirectory}} {
		if field.actual != field.expected {
			return fmt.Errorf("scheduled task Exec.%s mismatch", field.name)
		}
	}
	return nil
}
