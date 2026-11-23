package providersource

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/ledongthuc/pdf"
	gopdf "github.com/razvandimescu/gopdf/pdf"
)

const maxExtractedPDFText = 32 << 20

type pdfWord struct {
	Text string
	X    float64
	Y    float64
	W    float64
	Size float64
}

type pdfLine struct {
	Page  int
	Y     float64
	Words []pdfWord
}

type pdfSegment struct {
	Text string
	X    float64
}

func extractPDFText(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("PDF is empty")
	}
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("opening PDF: %w", err)
	}
	plain, err := reader.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("extracting PDF text: %w", err)
	}
	content, err := io.ReadAll(io.LimitReader(plain, maxExtractedPDFText+1))
	if err != nil {
		return "", fmt.Errorf("reading PDF text: %w", err)
	}
	if len(content) > maxExtractedPDFText {
		return "", errors.New("extracted PDF text exceeds 32 MiB")
	}
	text := strings.TrimSpace(string(content))
	if text == "" {
		return "", errors.New("PDF contains no extractable text")
	}
	return text, nil
}

func extractPDFLines(data []byte) ([]pdfLine, error) {
	if len(data) == 0 {
		return nil, errors.New("PDF is empty")
	}
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("opening PDF: %w", err)
	}
	var result []pdfLine
	positionedTextBytes := 0
	for pageNumber := 1; pageNumber <= reader.NumPage(); pageNumber++ {
		content := reader.Page(pageNumber).Content().Text
		var lines []pdfLine
		for _, item := range content {
			value := strings.ReplaceAll(item.S, "\n", "")
			if value == "" || strings.ContainsRune(value, '\ufffd') {
				continue
			}
			positionedTextBytes += len(value)
			if positionedTextBytes > maxExtractedPDFText {
				return nil, errors.New("positioned PDF text exceeds 32 MiB")
			}
			lineIndex := -1
			for index := range lines {
				if math.Abs(lines[index].Y-item.Y) <= 0.8 {
					lineIndex = index
					break
				}
			}
			if lineIndex < 0 {
				lines = append(lines, pdfLine{Page: pageNumber, Y: item.Y})
				lineIndex = len(lines) - 1
			}
			lines[lineIndex].Words = append(lines[lineIndex].Words, pdfWord{Text: value, X: item.X, Y: item.Y, W: item.W})
		}
		sort.Slice(lines, func(i, j int) bool { return lines[i].Y > lines[j].Y })
		for index := range lines {
			sort.SliceStable(lines[index].Words, func(i, j int) bool { return lines[index].Words[i].X < lines[index].Words[j].X })
		}
		result = append(result, lines...)
	}
	if len(result) == 0 {
		return nil, errors.New("PDF contains no extractable positioned text")
	}
	return result, nil
}

func extractPDFLinesWithXObjects(data []byte) ([]pdfLine, error) {
	if len(data) == 0 {
		return nil, errors.New("PDF is empty")
	}
	document, err := gopdf.OpenBytes(data)
	if err != nil {
		return nil, fmt.Errorf("opening PDF with Form XObject support: %w", err)
	}
	var result []pdfLine
	positionedTextBytes := 0
	for pageIndex := 0; pageIndex < document.NumPages(); pageIndex++ {
		spans, err := document.Page(pageIndex).TextSpans()
		if err != nil {
			return nil, fmt.Errorf("extracting PDF page %d with Form XObject support: %w", pageIndex+1, err)
		}
		var lines []pdfLine
		for _, span := range spans {
			value := strings.ReplaceAll(span.Text, "\n", "")
			if value == "" || strings.ContainsRune(value, '\ufffd') {
				continue
			}
			positionedTextBytes += len(value)
			if positionedTextBytes > maxExtractedPDFText {
				return nil, errors.New("positioned PDF text exceeds 32 MiB")
			}
			lineIndex := -1
			for index := range lines {
				if math.Abs(lines[index].Y-span.Y) <= 3.0 {
					lineIndex = index
					break
				}
			}
			if lineIndex < 0 {
				lines = append(lines, pdfLine{Page: pageIndex + 1, Y: span.Y})
				lineIndex = len(lines) - 1
			}
			lines[lineIndex].Words = append(lines[lineIndex].Words, pdfWord{
				Text: value, X: span.X, Y: span.Y, W: math.Max(0, span.EndX-span.X), Size: span.FontSize,
			})
		}
		sort.Slice(lines, func(i, j int) bool { return lines[i].Y > lines[j].Y })
		for index := range lines {
			sort.SliceStable(lines[index].Words, func(i, j int) bool { return lines[index].Words[i].X < lines[index].Words[j].X })
		}
		result = append(result, lines...)
	}
	if len(result) == 0 {
		return nil, errors.New("PDF contains no extractable positioned text")
	}
	return result, nil
}

func segmentPDFLine(line pdfLine, minimumGap float64) []pdfSegment {
	var result []pdfSegment
	var builder strings.Builder
	segmentX := 0.0
	previousEnd := 0.0
	flush := func() {
		value := strings.TrimSpace(builder.String())
		if value != "" {
			result = append(result, pdfSegment{Text: strings.Join(strings.Fields(value), " "), X: segmentX})
		}
		builder.Reset()
	}
	for _, word := range line.Words {
		if builder.Len() > 0 && word.X-previousEnd >= minimumGap {
			flush()
		}
		if builder.Len() == 0 {
			segmentX = word.X
		}
		builder.WriteString(word.Text)
		previousEnd = math.Max(previousEnd, word.X+word.W)
	}
	flush()
	return result
}
