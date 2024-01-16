package configuration

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	ListenAddress   string            `json:"listen_address"`
	DefaultResponse *DefaultResponse  `json:"default_response"`
	Domains         map[string]Domain `json:"domains" note:"Keys must be valid fully qualified DNS domain names in ASCII lower case and punycode if required."`
}

type DefaultResponse struct {
	Code    int               `json:"code"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	LogHits bool              `json:"log_hits"`
}

type Domain struct {
	RewriteRules    []Rule           `json:"rewrites"`
	DefaultResponse *DefaultResponse `json:"default_response"`
	MatchSubdomains bool             `json:"match_subdomains"`
}

type Rule struct {
	Regexp      *regexp.Regexp `json:"regexp"`
	Replacement string         `json:"replacement"`
	Code        int            `json:"code"`
	LogHits     bool           `json:"log_hits"`
}

// ruleWithPrimitiveValuesForUnmarshalling is used to unmarshal the JSON config file into a Rule.
// It is not exported because it is only used for unmarshalling.
// It must precisely match the structure of Rule, except that Regexp is a string instead of a *regexp.Regexp.
type ruleWithPrimitiveValuesForUnmarshalling struct {
	Regexp      string `json:"regexp"`
	Replacement string `json:"replacement"`
	Code        int    `json:"code"`
	LogHits     bool   `json:"log_hits"`
}

func (r *Rule) UnmarshalJSON(data []byte) error {
	var temp ruleWithPrimitiveValuesForUnmarshalling

	err := json.Unmarshal(data, &temp)
	if err != nil {
		return err
	}

	if r.Regexp, err = regexp.Compile(temp.Regexp); err != nil {
		return err
	}

	r.Replacement = temp.Replacement
	r.Code = temp.Code
	r.LogHits = temp.LogHits

	return nil
}

func LoadConfig(file io.Reader, config *Config) []string {
	var problems []string
	var origins []string

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()

	err := decoder.Decode(&config)
	if err != nil {
		return []string{fmt.Sprintf("Error parsing config file: %v", err)}
	}

	for origin, domain := range config.Domains {
		problems = append(problems, validateDomain(origin, domain)...)
		origins = append(origins, origin)
	}

	for origin, domain := range config.Domains {
		if domain.MatchSubdomains {
			for _, possible_subdomain := range origins {
				if strings.HasSuffix(strings.ToLower(possible_subdomain), "."+origin) {
					problems = append(problems, fmt.Sprintf("Domain %s has match_subdomains set to true, which makes the definition of subdomain %s prohibited", origin, possible_subdomain))
				}
			}
		}
	}

	if config.DefaultResponse != nil {
		validateDefaultResponse(config.DefaultResponse)
	}

	return problems
}

func validateDomain(origin string, domain Domain) []string {
	var problems []string
	domainRegex := regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?::\d+)?$`)

	if !domainRegex.MatchString(origin) {
		problems = append(problems, fmt.Sprintf("Invalid domain %s. Keys must be valid fully qualified DNS domain names in ASCII lowercase (in punycode if required), optionally including a port number.", origin))
	}

	for index, rewriteRule := range domain.RewriteRules {
		problems = append(problems, validateRule(origin, index, rewriteRule)...)
	}

	if domain.DefaultResponse != nil {
		validateDefaultResponse(domain.DefaultResponse)
	}

	return problems
}

func validateDefaultResponse(defaultResponse *DefaultResponse) []string {
	var problems []string

	if defaultResponse.Code != 0 && (defaultResponse.Code < 200 || defaultResponse.Code > 599) {
		problems = append(problems, fmt.Sprintf("Invalid default response code %d. Code must be between 200 and 599 inclusive, or 0 to close the connection immediately.", defaultResponse.Code))
	}

	return problems
}

func validateRule(origin string, index int, rewriteRule Rule) []string {
	var problems []string
	replacementRegex := regexp.MustCompile(`\$(\S+)`)

	if rewriteRule.Code < 300 || rewriteRule.Code > 399 {
		problems = append(problems, fmt.Sprintf("Invalid redirect code for domain %s at index %d. Code must be between 300 and 399 inclusive.", origin, index))
	}

	if !strings.HasPrefix(rewriteRule.Replacement, "http://") && !strings.HasPrefix(rewriteRule.Replacement, "https://") {
		problems = append(problems, fmt.Sprintf("Invalid replacement for domain %s at index %d. Destination must begin with 'http://' or 'https://'.", origin, index))
	}

	// Drop all "$$" so we're only matching things that aren't literal "$"s in the replacement string
	matches := replacementRegex.FindAllString(strings.ReplaceAll(rewriteRule.Replacement, "$$", ""), -1)
	for _, match := range matches {
		if replacement, err := strconv.ParseInt(match[1:], 10, 0); err != nil {
			problems = append(problems, fmt.Sprintf("Invalid replacement '%s' for domain %s at index %d. Only numbered replacements are supported: %v", rewriteRule.Replacement, origin, index, err))
		} else if int(replacement) < 0 || int(replacement) > rewriteRule.Regexp.NumSubexp() {
			problems = append(problems, fmt.Sprintf("Invalid replacement '%s' for domain %s at index %d: replacement group $%d does not exist", rewriteRule.Replacement, origin, index, replacement))
		}
	}

	return problems
}
