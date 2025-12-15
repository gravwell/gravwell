package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"

	"github.com/gravwell/gravwell/v3/sflow/datagram"
	"github.com/gravwell/gravwell/v3/sflow/internal/cmd/testgen/assets"
)

var ErrFixtureExists = errors.New("fixture file already exists (use -force to overwrite)")

// writeFixtures writes both .bin and .go files. basePath should NOT include an extension.
func writeFixtures(basePath string, packetData, goFileBuf []byte) error {
	binPath := basePath + ".bin"
	goPath := basePath + "_test.go"

	// Check if files exist (unless force is set)
	if !force {
		if _, err := os.Stat(binPath); err == nil {
			return fmt.Errorf("%s: %w", binPath, ErrFixtureExists)
		}
		if _, err := os.Stat(goPath); err == nil {
			return fmt.Errorf("%s: %w", goPath, ErrFixtureExists)
		}
	}

	if err := os.WriteFile(binPath, packetData, 0644); err != nil {
		return fmt.Errorf("failed to write bin file: %w", err)
	}

	if err := os.WriteFile(goPath, goFileBuf, 0644); err != nil {
		return fmt.Errorf("failed to write go file: %w", err)
	}

	return nil
}

// generateGoFixture generates the Go fixture file content. baseName is the filename without path or extension.
func generateGoFixture(baseName string, dgram *datagram.Datagram) ([]byte, error) {
	var buf bytes.Buffer

	// Write Go file header
	buf.Write(assets.LICENSEHeader)
	buf.WriteString("\n\n// Auto-generated code\n\n")
	buf.WriteString("package tests\n\n")
	buf.WriteString("import (\n")
	buf.WriteString("\t\"bytes\"\n")
	buf.WriteString("\t_ \"embed\"\n\n")
	buf.WriteString("\t\"net\"\n")
	buf.WriteString("\t\"reflect\"\n")
	buf.WriteString("\t\"testing\"\n\n")
	buf.WriteString("\t\"github.com/gravwell/gravwell/v3/sflow/datagram\"\n")
	buf.WriteString("\t\"github.com/gravwell/gravwell/v3/sflow/decoder\"\n")
	buf.WriteString(")\n\n")

	// Write the raw packet bytes using go:embed
	fixtureName := sanitizeTokenName(baseName)
	bytesName := fmt.Sprintf("%sBytes", fixtureName)
	decodedName := fmt.Sprintf("%sDecoded", fixtureName)
	fmt.Fprintf(&buf, "//go:embed %s.bin\n", baseName)
	fmt.Fprintf(&buf, "var %s []byte\n\n", bytesName)

	// Write the decoded datagram with manual serialization
	fmt.Fprintf(&buf, "var %s = ", decodedName)
	serializeDatagram(&buf, dgram)
	buf.WriteString("\n")

	// Write the actual test
	fmt.Fprintf(&buf, "func Test%s(t *testing.T) {\n", fixtureName)
	fmt.Fprintf(&buf, "\td := decoder.NewDatagramDecoder(bytes.NewReader(%s))\n", bytesName)
	buf.WriteString("\ts, err := d.Decode()\n")
	buf.WriteString("\tif err != nil {\n")
	buf.WriteString("\t\tt.Fatal(err)\n")
	buf.WriteString("\t}\n\n")
	fmt.Fprintf(&buf, "\tif !reflect.DeepEqual(s, %s) {\n", decodedName)
	fmt.Fprintf(&buf, "\t\tt.Fatalf(\"Decoded datagram does not match expected value.\\nExpected: %%+v\\nGot: %%+v\\n\", %s, s)\n", decodedName)
	buf.WriteString("\t}\n")
	buf.WriteString("}\n")

	// Convert hex literals to decimal and format code for readability
	return formatCode(buf.Bytes())
}

// buildBaseName returns the base filename (no extension) for a datagram.
// Format: sflow_sample_{fmt}_record_{recs}_sample_{fmt}_record_{recs}[_unknown_record_{fmts}][_unknown_sample_{fmts}]
//
// Unknown records will appear at the end of it's corresponding sample format. Unknown samples at the very end of the file name.
func buildBaseName(dgram *datagram.Datagram) string {
	var (
		parts                []string
		unknownSampleFormats []string
	)

	parts = append(parts, "sflow")

	for _, sample := range dgram.Samples {
		sampleHeader := sample.GetHeader()

		if _, ok := sample.(*datagram.UnknownSample); ok {
			// Unknown samples have unknown records, no use continuing
			unknownSampleFormats = append(unknownSampleFormats, fmt.Sprintf("%d", sampleHeader.Format))
			continue
		}

		parts = append(parts, fmt.Sprintf("sample_%d", sampleHeader.Format))

		var records []datagram.Record
		switch s := sample.(type) {
		case *datagram.FlowSample:
			records = s.Records
		case *datagram.FlowSampleExpanded:
			records = s.Records
		case *datagram.CounterSample:
			records = s.Records
		case *datagram.CounterSampleExpanded:
			records = s.Records
		}

		var recFormats []string
		var unknownRecFormats []string
		for _, record := range records {
			recHeader := record.GetHeader()
			if _, ok := record.(*datagram.UnknownRecord); ok {
				unknownRecFormats = append(unknownRecFormats, fmt.Sprintf("%d", recHeader.Format))
			} else {
				recFormats = append(recFormats, fmt.Sprintf("%d", recHeader.Format))
			}
		}

		if len(recFormats) > 0 {
			parts = append(parts, "record_"+strings.Join(recFormats, "_"))
		}

		// Unknown records at the end of each sample part
		if len(unknownRecFormats) > 0 {
			parts = append(parts, "unknown_record_"+strings.Join(unknownRecFormats, "_"))
		}
	}

	// Unknown samples at the end
	if len(unknownSampleFormats) > 0 {
		parts = append(parts, "unknown_sample_"+strings.Join(unknownSampleFormats, "_"))
	}

	return strings.Join(parts, "_")
}

func serializeDatagram(buf *bytes.Buffer, dgram *datagram.Datagram) {
	buf.WriteString("&datagram.Datagram{\n")
	fmt.Fprintf(buf, "\tVersion:        %d,\n", dgram.Version)
	fmt.Fprintf(buf, "\tIPVersion:      %d,\n", dgram.IPVersion)
	fmt.Fprintf(buf, "\tAgentIP:        %#v,\n", dgram.AgentIP)
	fmt.Fprintf(buf, "\tSubAgentID:     %d,\n", dgram.SubAgentID)
	fmt.Fprintf(buf, "\tSequenceNumber: %d,\n", dgram.SequenceNumber)
	fmt.Fprintf(buf, "\tUptime:         %d,\n", dgram.Uptime)
	fmt.Fprintf(buf, "\tSamplesCount:   %d,\n", dgram.SamplesCount)

	// Serialize samples
	buf.WriteString("\tSamples: []datagram.Sample{\n")
	for _, sample := range dgram.Samples {
		serializeSample(buf, sample)
	}
	buf.WriteString("\t},\n")
	buf.WriteString("}")
}

func serializeSample(buf *bytes.Buffer, sample datagram.Sample) {
	switch s := sample.(type) {
	case *datagram.FlowSample:
		buf.WriteString("\t\t&datagram.FlowSample{\n")
		fmt.Fprintf(buf, "\t\t\tSampleHeader:    datagram.SampleHeader{Format: %d, Length: %d},\n", s.Format, s.Length)
		fmt.Fprintf(buf, "\t\t\tSequenceNum:     %d,\n", s.SequenceNum)
		fmt.Fprintf(buf, "\t\t\tSFlowDataSource: %d,\n", s.SFlowDataSource)
		fmt.Fprintf(buf, "\t\t\tSamplingRate:    %d,\n", s.SamplingRate)
		fmt.Fprintf(buf, "\t\t\tSamplePool:      %d,\n", s.SamplePool)
		fmt.Fprintf(buf, "\t\t\tDrops:           %d,\n", s.Drops)
		fmt.Fprintf(buf, "\t\t\tInput:           %d,\n", s.Input)
		fmt.Fprintf(buf, "\t\t\tOutput:          %d,\n", s.Output)
		serializeRecords(buf, s.Records)
		buf.WriteString("\t\t},\n")

	case *datagram.FlowSampleExpanded:
		buf.WriteString("\t\t&datagram.FlowSampleExpanded{\n")
		fmt.Fprintf(buf, "\t\t\tSampleHeader: datagram.SampleHeader{Format: %d, Length: %d},\n", s.Format, s.Length)
		fmt.Fprintf(buf, "\t\t\tSequenceNum:  %d,\n", s.SequenceNum)
		fmt.Fprintf(buf, "\t\t\tSFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: %d, SourceIDIndex: %d},\n",
			s.SourceIDType, s.SourceIDIndex)
		fmt.Fprintf(buf, "\t\t\tSamplingRate: %d,\n", s.SamplingRate)
		fmt.Fprintf(buf, "\t\t\tSamplePool:   %d,\n", s.SamplePool)
		fmt.Fprintf(buf, "\t\t\tDrops:        %d,\n", s.Drops)
		fmt.Fprintf(buf, "\t\t\tInput:        datagram.InterfaceExpanded{Format: %d, Value: %d},\n", s.Input.Format, s.Input.Value)
		fmt.Fprintf(buf, "\t\t\tOutput:       datagram.InterfaceExpanded{Format: %d, Value: %d},\n", s.Output.Format, s.Output.Value)
		serializeRecords(buf, s.Records)
		buf.WriteString("\t\t},\n")

	case *datagram.CounterSample:
		buf.WriteString("\t\t&datagram.CounterSample{\n")
		fmt.Fprintf(buf, "\t\t\tSampleHeader:    datagram.SampleHeader{Format: %d, Length: %d},\n", s.Format, s.Length)
		fmt.Fprintf(buf, "\t\t\tSequenceNum:     %d,\n", s.SequenceNum)
		fmt.Fprintf(buf, "\t\t\tSFlowDataSource: %d,\n", s.SFlowDataSource)
		serializeRecords(buf, s.Records)
		buf.WriteString("\t\t},\n")

	case *datagram.CounterSampleExpanded:
		buf.WriteString("\t\t&datagram.CounterSampleExpanded{\n")
		fmt.Fprintf(buf, "\t\t\tSampleHeader: datagram.SampleHeader{Format: %d, Length: %d},\n", s.Format, s.Length)
		fmt.Fprintf(buf, "\t\t\tSequenceNum:  %d,\n", s.SequenceNum)
		fmt.Fprintf(buf, "\t\t\tSFlowDataSourceExpanded: datagram.SFlowDataSourceExpanded{SourceIDType: %d, SourceIDIndex: %d},\n",
			s.SourceIDType, s.SourceIDIndex)
		serializeRecords(buf, s.Records)
		buf.WriteString("\t\t},\n")

	case *datagram.UnknownSample:
		buf.WriteString("\t\t&datagram.UnknownSample{\n")
		fmt.Fprintf(buf, "\t\t\tFormat: %d,\n", s.Format)
		buf.WriteString("\t\t\tData: datagram.XDRVariableLengthOpaque{")
		for i, b := range s.Data {
			if i > 0 {
				if i%16 == 0 {
					buf.WriteString(",\n\t\t\t\t")
				} else {
					buf.WriteString(", ")
				}
			}
			fmt.Fprintf(buf, "%d", b)
		}
		buf.WriteString("},\n")
		buf.WriteString("\t\t},\n")

	default:
		buf.WriteString("\t\tnil, // unknown sample type\n")
	}
}

func serializeRecords(buf *bytes.Buffer, records []datagram.Record) {
	buf.WriteString("\t\t\tRecords: []datagram.Record{\n")
	for _, record := range records {
		// Use %#v for each concrete record type
		buf.WriteString("\t\t\t\t")
		fmt.Fprintf(buf, "%#v", record)
		buf.WriteString(",\n")
	}
	buf.WriteString("\t\t\t},\n")
}

func sanitizeTokenName(name string) string {
	// Replace invalid characters with underscores
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	// Capitalize first letter
	if len(name) > 0 {
		name = strings.ToUpper(name[:1]) + name[1:]
	}
	return name
}

// formatCode parses Go source code and converts all hex integer literals to decimal.
func formatCode(sourceCode []byte) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", sourceCode, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse generated code: %w", err)
	}

	// Walk through AST and replace hex literals with decimal
	ast.Inspect(f, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.INT {
			if strings.HasPrefix(lit.Value, "0x") || strings.HasPrefix(lit.Value, "0X") {
				num, _ := strconv.ParseUint(lit.Value, 0, 64)
				lit.Value = fmt.Sprintf("%d", num)
			}
		}
		return true
	})

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, f); err != nil {
		return nil, fmt.Errorf("failed to format code: %w", err)
	}
	return buf.Bytes(), nil
}
