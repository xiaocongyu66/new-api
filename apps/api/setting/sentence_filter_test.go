package setting

import (
	"embed"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/stretchr/testify/assert"
)

//go:embed testdata/sentence_filter_test_input.json
var sentenceFilterTestFS embed.FS

//go:embed testdata/sensitive_words_from_string_test.json
var sensitiveWordsFromStringTestFS embed.FS

type sentenceFilterFixture struct {
	Test1Input    []string `json:"test1_input"`
	Test1Expected []string `json:"test1_expected"`
}

type sensitiveWordsFromStringFixture struct {
	Test2InputString string   `json:"test2_input_string"`
	Test2Expected    []string `json:"test2_expected"`
}

func loadSentenceFilterTest(t *testing.T) sentenceFilterFixture {
	t.Helper()
	data, err := sentenceFilterTestFS.ReadFile("testdata/sentence_filter_test_input.json")
	assert.NoError(t, err)
	var f sentenceFilterFixture
	assert.NoError(t, common.Unmarshal(data, &f))
	return f
}

func loadSensitiveWordsFromStringTest(t *testing.T) sensitiveWordsFromStringFixture {
	t.Helper()
	data, err := sensitiveWordsFromStringTestFS.ReadFile("testdata/sensitive_words_from_string_test.json")
	assert.NoError(t, err)
	var f sensitiveWordsFromStringFixture
	assert.NoError(t, common.Unmarshal(data, &f))
	return f
}

func TestFilterSensitiveWords(t *testing.T) {
	f := loadSentenceFilterTest(t)
	got := FilterSensitiveWords(f.Test1Input)
	assert.ElementsMatch(t, f.Test1Expected, got)
}

func TestSensitiveWordsFromStringFiltersShort(t *testing.T) {
	f := loadSensitiveWordsFromStringTest(t)
	old := SensitiveWords
	t.Cleanup(func() { SensitiveWords = old })

	SensitiveWordsFromString(f.Test2InputString)
	assert.ElementsMatch(t, f.Test2Expected, SensitiveWords)
}
