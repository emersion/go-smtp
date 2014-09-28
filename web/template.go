package web

import (
	"fmt"
	"html"
	"html/template"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gleez/smtpd/log"
	"github.com/microcosm-cc/bluemonday"
)

var cachedMutex sync.Mutex
var cachedTemplates = map[string]*template.Template{}
var cachedPartials = map[string]*template.Template{}

var TemplateFuncs = template.FuncMap{
	"htmlSafe":     htmlSafe,
	"friendlyTime": friendlyTime,
	"reverse":      reverse,
	"textToHtml":   textToHtml,
}

// From http://daringfireball.net/2010/07/improved_regex_for_matching_urls
var urlRE = regexp.MustCompile("(?i)\\b((?:[a-z][\\w-]+:(?:/{1,3}|[a-z0-9%])|www\\d{0,3}[.]|[a-z0-9.\\-]+[.][a-z]{2,4}/)(?:[^\\s()<>]+|\\(([^\\s()<>]+|(\\([^\\s()<>]+\\)))*\\))+(?:\\(([^\\s()<>]+|(\\([^\\s()<>]+\\)))*\\)|[^\\s`!()\\[\\]{};:'\".,<>?«»“”‘’]))")

// RenderTemplate fetches the named template and renders it to the provided
// ResponseWriter.
func RenderTemplate(name string, w http.ResponseWriter, data interface{}) error {
	t, err := ParseTemplate(name, false)
	if err != nil {
		log.LogError("Error in template '%v': %v", name, err)
		return err
	}

	w.Header().Set("Expires", "-1")
	return t.Execute(w, data)
}

// RenderPartial fetches the named template and renders it to the provided
// ResponseWriter.
func RenderPartial(name string, w http.ResponseWriter, data interface{}) error {
	t, err := ParseTemplate(name, true)
	if err != nil {
		log.LogError("Error in template '%v': %v", name, err)
		return err
	}
	w.Header().Set("Expires", "-1")
	return t.Execute(w, data)
}

// ParseTemplate loads the requested template along with _base.html, caching
// the result (if configured to do so)
func ParseTemplate(name string, partial bool) (*template.Template, error) {
	cachedMutex.Lock()
	defer cachedMutex.Unlock()

	if t, ok := cachedTemplates[name]; ok {
		return t, nil
	}

	tempPath := strings.Replace(name, "/", string(filepath.Separator), -1)
	tempFile := filepath.Join(webConfig.TemplateDir, tempPath)
	log.LogTrace("Parsing template %v", tempFile)

	var err error
	var t *template.Template
	if partial {
		// Need to get basename of file to make it root template w/ funcs
		base := path.Base(name)
		t = template.New(base).Funcs(TemplateFuncs)
		t, err = t.ParseFiles(tempFile)
	} else {
		t = template.New("layout.html").Funcs(TemplateFuncs)
		// Note that the layout file must be the first parameter in ParseFiles
		t, err = t.ParseFiles(filepath.Join(webConfig.TemplateDir, "layout.html"), tempFile)
	}
	if err != nil {
		return nil, err
	}

	// Allows us to disable caching for theme development
	if webConfig.TemplateCache {
		if partial {
			log.LogTrace("Caching partial %v", name)
			cachedTemplates[name] = t
		} else {
			log.LogTrace("Caching template %v", name)
			cachedTemplates[name] = t
		}
	}

	return t, nil
}

// HTML rendering
func htmlSafe(text string) template.HTML {
	p := bluemonday.UGCPolicy()
	txt := p.Sanitize(text)
	return template.HTML(txt)
}

// Friendly date & time rendering
func friendlyTime(t time.Time) template.HTML {
	ty, tm, td := t.Date()
	ny, nm, nd := time.Now().Date()
	if (ty == ny) && (tm == nm) && (td == nd) {
		return template.HTML(t.Format("03:04:05 PM"))
	}
	return template.HTML(t.Format("Mon Jan 2, 2006"))
}

// textToHtml takes plain text, escapes it and tries to pretty it up for
// HTML display
func textToHtml(text string) template.HTML {
	text = html.EscapeString(text)
	text = urlRE.ReplaceAllStringFunc(text, wrapUrl)
	replacer := strings.NewReplacer("\r\n", "<br/>\n", "\r", "<br/>\n", "\n", "<br/>\n")
	return template.HTML(replacer.Replace(text))
}

// wrapUrl wraps a <a href> tag around the provided URL
func wrapUrl(url string) string {
	unescaped := strings.Replace(url, "&amp;", "&", -1)
	return fmt.Sprintf("<a href=\"%s\" target=\"_blank\">%s</a>", unescaped, url)
}

// Reversable routing function (shared with templates)
func reverse(name string, things ...interface{}) string {
	// Convert the things to strings
	strs := make([]string, len(things))
	for i, th := range things {
		strs[i] = fmt.Sprint(th)
	}
	// Grab the route
	u, err := Router.Get(name).URL(strs...)
	if err != nil {
		log.LogError("Failed to reverse route: %v", err)
		return "/ROUTE-ERROR"
	}
	return u.Path
}
