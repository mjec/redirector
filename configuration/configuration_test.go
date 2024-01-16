package configuration

import (
	"bytes"
	"reflect"
	"regexp"
	"testing"
)

func TestLoadConfigOK(t *testing.T) {
	jsonData := []byte(`{
		"listen_address": ":8080",
		"default_response": {
			"code": 421,
			"body": "421 Misdirected Request\n\nTarget URI does not match an origin for which the server has been configured.\n",
			"headers": {
				"Connection":   "close",
				"Content-Type": "text/plain"
			},
			"log_hits": true
		},
		"domains": {
			"example.com": {
				"match_subdomains": true,
				"rewrites": [
					{
						"regexp": "^(.*)$",
						"replacement": "https://www.example.com$1",
						"code": 301,
						"log_hits": true
					}
				]
			}
		}
	}`)

	config := &Config{}
	if problems := LoadConfig(bytes.NewReader(jsonData), config); len(problems) != 0 {
		t.Errorf("Expected no problems, but got %d problems: %v", len(problems), problems)
	}
}

func TestLoadConfigMultipleSubdomainMatch(t *testing.T) {
	jsonData := []byte(`{
		"listen_address": ":8080",
		"default_response": {
			"code": 421,
			"body": "421 Misdirected Request\n\nTarget URI does not match an origin for which the server has been configured.\n",
			"headers": {
				"Connection":   "close",
				"Content-Type": "text/plain"
			},
			"log_hits": true
		},
		"domains": {
			"example.com": {
				"match_subdomains": true,
				"rewrites": [
					{
						"regexp": "^(.*)$",
						"replacement": "https://www.example.com$1",
						"code": 301,
						"log_hits": true
					}
				]
			},
			"www.example.com": {}
		}
	}`)

	config := &Config{}
	if problems := LoadConfig(bytes.NewReader(jsonData), config); len(problems) != 1 {
		t.Errorf("Expected 1 problem (defining www.example.com is prohibited because example.com has match_subdomains = true), but got %d problems: %v", len(problems), problems)
	}
}

func TestLoadConfigInvalidJSON(t *testing.T) {
	jsonData := []byte(`invalid json`)

	config := &Config{}
	if problems := LoadConfig(bytes.NewReader(jsonData), config); len(problems) != 1 {
		t.Errorf("Expected 1 problem (invalid JSON), but got %d problems: %v", len(problems), problems)
	}
}

func TestLoadConfigUnknownField(t *testing.T) {
	jsonData := []byte(`{"not_a_valid_field": "value"}`)

	config := &Config{}
	if problems := LoadConfig(bytes.NewReader(jsonData), config); len(problems) != 1 {
		t.Errorf("Expected 1 problem (invalid JSON), but got %d problems: %v", len(problems), problems)
	}
}

func TestUnmarshalRule(t *testing.T) {
	// Define a sample JSON payload
	jsonData := []byte(`{
		"regexp": "example.com",
		"replacement": "www.example.com",
		"code": 301,
		"log_hits": true
	}`)

	// Create a new Rule instance
	rule := &Rule{}

	// Unmarshal the JSON payload into the Rule instance
	err := rule.UnmarshalJSON(jsonData)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify the unmarshalled values
	expectedRegexp := regexp.MustCompile("example.com")
	if !reflect.DeepEqual(rule.Regexp, expectedRegexp) {
		t.Errorf("Expected regexp to be %v, but got %v", expectedRegexp, rule.Regexp)
	}

	expectedReplacement := "www.example.com"
	if rule.Replacement != expectedReplacement {
		t.Errorf("Expected replacement to be %s, but got %s", expectedReplacement, rule.Replacement)
	}

	expectedCode := 301
	if rule.Code != expectedCode {
		t.Errorf("Expected code to be %d, but got %d", expectedCode, rule.Code)
	}

	expectedLogHits := true
	if rule.LogHits != expectedLogHits {
		t.Errorf("Expected log_hits to be %t, but got %t", expectedLogHits, rule.LogHits)
	}
}

func TestUnmarshalRuleDefaults(t *testing.T) {
	jsonData := []byte(`{}`)

	// Create a new Rule instance
	rule := &Rule{}

	// Unmarshal the JSON payload into the Rule instance
	err := rule.UnmarshalJSON(jsonData)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify the unmarshalled values
	expectedRegexp := regexp.MustCompile("")
	if !reflect.DeepEqual(rule.Regexp, expectedRegexp) {
		t.Errorf("Expected regexp to be %v, but got %v", expectedRegexp, rule.Regexp)
	}

	expectedReplacement := ""
	if rule.Replacement != expectedReplacement {
		t.Errorf("Expected replacement to be %s, but got %s", expectedReplacement, rule.Replacement)
	}

	expectedCode := 0
	if rule.Code != expectedCode {
		t.Errorf("Expected code to be %d, but got %d", expectedCode, rule.Code)
	}

	expectedLogHits := false
	if rule.LogHits != expectedLogHits {
		t.Errorf("Expected log_hits to be %t, but got %t", expectedLogHits, rule.LogHits)
	}
}

func TestUnmarshalRuleFailures(t *testing.T) {
	jsonData := []byte(`not valid json`)
	rule := &Rule{}
	if err := rule.UnmarshalJSON(jsonData); err == nil {
		t.Errorf("Expected error decoding invalid JSON but got none: %v", err)
	}

	jsonData = []byte(`{
		"regexp": "some (unterminated parenthesis"
	}`)
	if err := rule.UnmarshalJSON(jsonData); err == nil {
		t.Errorf("Expected error compiling rule regexp but got none: %v", err)
	}
}

func TestValidateDomain(t *testing.T) {
	domain := Domain{
		RewriteRules: []Rule{},
	}

	if problems := validateDomain("example.com", domain); len(problems) != 0 {
		t.Errorf("Expected no problems, but got %d problems: %v", len(problems), problems)
	}

	if problems := validateDomain("example.com:1234", domain); len(problems) != 0 {
		t.Errorf("Expected no problems, but got %d problems: %v", len(problems), problems)
	}

	if problems := validateDomain("xn--qwc.example.com:1234", domain); len(problems) != 0 {
		t.Errorf("Expected no problems, but got %d problems: %v", len(problems), problems)
	}

	if problems := validateDomain("xn--qwc.example.com", domain); len(problems) != 0 {
		t.Errorf("Expected no problems, but got %d problems: %v", len(problems), problems)
	}

	if problems := validateDomain("not-a-valid-fqdn", domain); len(problems) != 1 {
		t.Errorf("Expected one problem (invalid domain name), but got %d problems: %v", len(problems), problems)
	}

	if problems := validateDomain("-must-start-reasonably.invalid", domain); len(problems) != 1 {
		t.Errorf("Expected one problem (invalid domain name), but got %d problems: %v", len(problems), problems)
	}

	if problems := validateDomain("example.com.", domain); len(problems) != 1 {
		t.Errorf("Expected one problem (invalid domain name), but got %d problems: %v", len(problems), problems)
	}

	if problems := validateDomain("http://example.com", domain); len(problems) != 1 {
		t.Errorf("Expected one problem (invalid domain name), but got %d problems: %v", len(problems), problems)
	}

	if problems := validateDomain("also.not_valid", domain); len(problems) != 1 {
		t.Errorf("Expected one problem ('_' character prohibited), but got %d problems: %v", len(problems), problems)
	}

	if problems := validateDomain("Example.com", domain); len(problems) != 1 {
		t.Errorf("Expected one problem (uppercase letters prohibited), but got %d problems: %v", len(problems), problems)
	}

	domain.DefaultResponse = &DefaultResponse{
		Code: 0,
	}
	if problems := validateDomain("example.com", domain); len(problems) != 0 {
		t.Errorf("Expected no problems, but got %d problems: %v", len(problems), problems)
	}

	domain.DefaultResponse = &DefaultResponse{
		Code: -1,
	}
	if problems := validateDomain("example.com", domain); len(problems) != 0 {
		t.Errorf("Expected no problems, but got %d problems: %v", len(problems), problems)
	}

	domain.DefaultResponse = nil
	domain.RewriteRules = append(
		domain.RewriteRules,
		Rule{
			Regexp:      regexp.MustCompile("pattern"),
			Replacement: "https://www.example.com/",
			Code:        301,
			LogHits:     true,
		},
	)
	if problems := validateDomain("example.com", domain); len(problems) != 0 {
		t.Errorf("Expected no problems, but got %d problems: %v", len(problems), problems)
	}
	domain.RewriteRules[0].Replacement = "" // invalid!
	if problems := validateDomain("example.com", domain); len(problems) != 1 {
		t.Errorf("Expected one problem (invalid rewrite rule), but got %d problems: %v", len(problems), problems)
	}
}

func TestValidateDefaultResponse(t *testing.T) {
	defaultResponse := &DefaultResponse{
		Code: 200,
	}
	if problems := validateDefaultResponse(defaultResponse); len(problems) != 0 {
		t.Errorf("Expected no problems, but got %d problems: %v", len(problems), problems)
	}

	defaultResponse = &DefaultResponse{
		Code: 0,
	}
	if problems := validateDefaultResponse(defaultResponse); len(problems) != 0 {
		t.Errorf("Expected no problems, but got %d problems: %v", len(problems), problems)
	}

	defaultResponse = &DefaultResponse{
		Code: -1,
	}
	if problems := validateDefaultResponse(defaultResponse); len(problems) == 0 {
		t.Errorf("Expected one problem (code = -1 is invalid), but got %d problems: %v", len(problems), problems)
	}

	defaultResponse = &DefaultResponse{
		Code: 99,
	}
	if problems := validateDefaultResponse(defaultResponse); len(problems) == 0 {
		t.Errorf("Expected one problem (code = 99 is invalid), but got %d problems: %v", len(problems), problems)
	}

	defaultResponse = &DefaultResponse{
		Code: 600,
	}
	if problems := validateDefaultResponse(defaultResponse); len(problems) == 0 {
		t.Errorf("Expected one problem (code = 600 is invalid), but got %d problems: %v", len(problems), problems)
	}
}

func TestValidateRule(t *testing.T) {
	origin := "example.com"
	index := 0
	rewriteRule := Rule{
		Code:        301,
		Replacement: "http://example.com",
		Regexp:      regexp.MustCompile("pattern"),
	}

	problems := validateRule(origin, index, rewriteRule)

	if len(problems) != 0 {
		t.Errorf("Expected no problems, but got %d problems: %v", len(problems), problems)
	}

	rewriteRule.Code = 400
	problems = validateRule(origin, index, rewriteRule)

	if len(problems) != 1 {
		t.Errorf("Expected 1 problem, but got %d problems: %v", len(problems), problems)
	}

	rewriteRule.Code = 301
	rewriteRule.Replacement = "example.com"
	problems = validateRule(origin, index, rewriteRule)

	if len(problems) != 1 {
		t.Errorf("Expected 1 problem, but got %d problems: %v", len(problems), problems)
	}

	rewriteRule.Replacement = "http://example.com/$0"
	rewriteRule.Regexp = regexp.MustCompile(`no subpatterns at all`)
	problems = validateRule(origin, index, rewriteRule)

	if len(problems) != 0 {
		t.Errorf("Expected no problems, but got %d problems: %v", len(problems), problems)
	}

	rewriteRule.Replacement = "http://example.com/$1"
	rewriteRule.Regexp = regexp.MustCompile(`no subpatterns at all`)
	problems = validateRule(origin, index, rewriteRule)

	if len(problems) != 1 {
		t.Errorf("Expected 1 problem, but got %d problems: %v", len(problems), problems)
	}

	rewriteRule.Replacement = "http://example.com/$$1"
	rewriteRule.Regexp = regexp.MustCompile(`no subpatterns at all`)
	problems = validateRule(origin, index, rewriteRule)

	if len(problems) != 0 {
		t.Errorf("Expected no problems, but got %d problems: %v", len(problems), problems)
	}

	rewriteRule.Replacement = "http://example.com/$$2"
	rewriteRule.Regexp = regexp.MustCompile(`one (subpattern) to speak of`)
	problems = validateRule(origin, index, rewriteRule)
	if len(problems) != 0 {
		t.Errorf("Expected no problems, but got %d problems: %v", len(problems), problems)
	}

	rewriteRule.Replacement = "http://example.com/$$$2"
	rewriteRule.Regexp = regexp.MustCompile(`one (subpattern) to speak of`)
	problems = validateRule(origin, index, rewriteRule)
	if len(problems) != 1 {
		t.Errorf("Expected 1 problem, but got %d problems: %v", len(problems), problems)
	}

	rewriteRule.Replacement = "http://example.com/${name}"
	rewriteRule.Regexp = regexp.MustCompile(`one (?P<name>subpattern) to speak of`)
	problems = validateRule(origin, index, rewriteRule)
	if len(problems) != 1 {
		t.Errorf("Expected 1 problem, but got %d problems: %v", len(problems), problems)
	}
}

func TestRuleTypeMatchesRuleWithPrimitiveValuesForUnmarshalling(t *testing.T) {
	simple := reflect.TypeOf(ruleWithPrimitiveValuesForUnmarshalling{})
	actual := reflect.TypeOf(Rule{})

	if actual.NumField() != simple.NumField() {
		t.Errorf("Rule has %d fields, but ruleWithPrimitiveValuesForUnmarshalling has %d fields; they should precisely match", actual.NumField(), simple.NumField())
		return
	}

	for idx := 0; idx < actual.NumField(); idx++ {
		field := actual.Field(idx)
		if field.Name != simple.Field(idx).Name {
			t.Errorf("Field at index %d of ruleWithPrimitiveValuesForUnmarshalling has name %s, but should have name %s to match Rule", idx, simple.Field(idx).Name, field.Name)
		} else if field.Type == reflect.TypeOf((*regexp.Regexp)(nil)) {
			if simple.Field(idx).Type.Kind() != reflect.String {
				t.Errorf("Field %s of ruleWithPrimitiveValuesForUnmarshalling has type %s, but should be a string to match Rule's *regexp.Regexp", simple.Field(idx).Name, simple.Field(idx).Type)
			}
		} else if field.Type != simple.Field(idx).Type {
			t.Errorf("Field %s of ruleWithPrimitiveValuesForUnmarshalling has type %s, but should have type %s to match Rule", simple.Field(idx).Name, simple.Field(idx).Type, field.Type)
		}
	}

}
