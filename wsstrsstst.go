package main

import (
	"compress/bzip2"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
	//"sync"
)

// For XML processing of Språkbanken's corpus files
type Sentence struct {
	Sentence string `xml:"text>sentence"`
}

type Text struct {
	Text      string     `xml:"corpus>text"`
	Sentences []Sentence //`xml:text>sentence`
	Word      string     `xml:w`
}

type W struct {
	POS string `xml:"pos,attr"`
	W   string `xml:",innerxml"`
}

// The element names of Språkbanken's corpus XML
var knownElems = map[string]bool{
	"corpus":   true,
	"text":     true,
	"sentence": true,
	"ne":       true,
	"w":        true,
}

// For sending and receiving sentences to the tts server
type sent struct {
	lang string
	text string
	n    int
	err  error
	l    int
}

// Max number of concurrent calls to tts server
var maxConcurr = 10

// Number of calls before increasing the number of concurrent calls with one
var incEvery = 100

func readSents(corpusFile string, response chan sent) {
	if _, err := os.Stat(corpusFile); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "Input file does not exist:", corpusFile)
		os.Exit(1)
	} else if strings.HasSuffix(corpusFile, ".xml.bz2") {
		readSentsSBXml(corpusFile, response)
	} else if strings.HasSuffix(corpusFile, ".xml") {
		readSentsSBXml(corpusFile, response)
	} else if strings.HasSuffix(corpusFile, ".txt") {
		readSentsTxt(corpusFile, response)
	} else {
		fmt.Fprintln(os.Stderr, "Unknown file type:", corpusFile)
		os.Exit(1)
	}
}

func readSentsTxt(corpusFile string, response chan sent) {
	content, err := ioutil.ReadFile(corpusFile)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	var n int
	lines := strings.Split(string(content), "\n")
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n++

		s := sent{lang: lang, text: l, n: n}
		response <- s
	}
	close(response)
}

func readSentsSBXml(corpusFile string, response chan sent) {
	var xmlFile *os.File
	xmlFile, err := os.Open(corpusFile)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer xmlFile.Close()

	var decoder *xml.Decoder
	if strings.HasSuffix(corpusFile, ".bz2") {
		bz := bzip2.NewReader(xmlFile)
		decoder = xml.NewDecoder(bz)
	} else {
		decoder = xml.NewDecoder(xmlFile)
	}

	var words []string
	var n int
	for {
		// Read tokens from the XML document in a stream.
		t, _ := decoder.Token()
		if t == nil {
			break
		}
		// Inspect the type of the token just read.
		switch se := t.(type) {
		case xml.EndElement:
			if se.Name.Local == "sentence" {

				text := strings.Join(words, " ")
				words = nil
				n++

				s := sent{lang: "sv", text: text, n: n}
				//sents = append(sents, s)
				response <- s
			}
		case xml.StartElement:
			elemName := se.Name.Local
			if !knownElems[elemName] {
				fmt.Printf("Unknown element: %s\n", elemName)
				os.Exit(1)
			}
			if elemName == "w" {
				w := W{}
				decoder.DecodeElement(&w, &se)
				words = append(words, w.W)
			}
		default: // ?
		}

	}

	close(response)
}

var audioDir = "audio"
var saveAudio bool = false // save audio file locally
var nMax = 0               // max no of sentences to synthesize
var finalSlashRe = regexp.MustCompile("/$")
var wikispeechURL = "<undefined>"
var lang = "<undefined>"

func callSynthN(sents []sent) []sent {
	var res []sent

	resp := make(chan sent)

	for _, s := range sents {
		go callSynth1(s, resp)
	}

	for i := 0; i < len(sents); i++ {
		rez := <-resp
		res = append(res, rez)
		if rez.err != nil {
			return res
		}
	}

	return res
}

// For unmarshalling JSON from tts server
type Token struct {
	Endtime float64 `json:"endtime"`
	Orth    string  `json:"orth"`
}
type WSResponse struct {
	Audio  string  `json:"audio"`
	Tokens []Token `json:"tokens"`
}

func callSynth1(s sent, resp chan sent) {

	res := sent{lang: s.lang, text: s.text, n: s.n}

	urrl := wikispeechURL + "/?lang=" + s.lang + "&input=" + url.QueryEscape(s.text)
	ures, err := http.Get(urrl)
	if err != nil {
		res.err = err
		resp <- res
		return
	}

	body, err := ioutil.ReadAll(ures.Body)
	defer ures.Body.Close()
	if err != nil {

		res.err = fmt.Errorf("failed to get URL : %v", err)
		resp <- res
		return //fmt.Errorf("failed to get URL : %v", err)
	}

	var wsResp WSResponse
	err = json.Unmarshal(body, &wsResp)
	if err != nil {
		res.err = fmt.Errorf("failed to unmarshal json %s : %v", string(body), err)
		resp <- res
		return //fmt.Errorf("failed to unmarshal json %s : %v", string(body), err)
	}

	aRes, err := http.Get(wsResp.Audio)
	if err != nil {
		res.err = fmt.Errorf("failed to get audio : %v", err)
		resp <- res
		return //fmt.Errorf("failed to get audio : %v", err)
	}
	aBody, err := ioutil.ReadAll(aRes.Body)
	if err != nil {
		res.err = fmt.Errorf("failed to read response : %v", err)
		resp <- res
		return //fmt.Errorf("failed to write audio file : %v", err)
	}

	u, err := url.Parse(wsResp.Audio)
	if err != nil {
		res.err = fmt.Errorf("failed to parse audio url : %v", err)
		resp <- res
		return //fmt.Errorf("failed to write audio file : %v", err)
	}
	if saveAudio {
		ioutil.WriteFile(filepath.Join(audioDir, filepath.Base(u.Path)), aBody, 0755)
	}

	res.l = len(aBody)
	resp <- res

}

func main() {

	var nMaxF = flag.Int("n", 0, "max number of sentences to synthesize (default no limit)")
	var saveAudioF = flag.Bool("a", false, "save audio files to disk (default false)")
	var wikispeechURLF = flag.String("u", "http://localhost:10000", "wikispeech url")
	var langF = flag.String("l", "sv", "wikispeech language tag")

	var printUsage = func() {
		fmt.Fprintln(os.Stderr, "go run wsstrsstst.go <flags> <Text file> (one sentence per line)")
		fmt.Fprintln(os.Stderr, " OR")
		fmt.Fprintln(os.Stderr, "go run wsstrsstst.go <flags> <Språkbanken corpus file>\n - See https://spraakbanken.gu.se/eng/resources. The file can be in .bz2 or unzipped XML.")
		fmt.Fprintln(os.Stderr, "\nOptional flags:")
		flag.PrintDefaults()
	}

	flag.Usage = func() {
		printUsage()
		os.Exit(0)
	}

	flag.Parse()

	nMax = *nMaxF
	saveAudio = *saveAudioF
	wikispeechURL = finalSlashRe.ReplaceAllString(*wikispeechURLF, "")
	lang = *langF

	if flag.NArg() != 1 {
		printUsage()
		os.Exit(0)
	}

	file := flag.Arg(0)

	nMaxS := fmt.Sprintf("%d", nMax)
	if nMax == 0 {
		nMaxS = "no limit"
	}
	saveAudioS := ": no"
	if saveAudio {
		saveAudioS = fmt.Sprintf(" in folder: %s", audioDir)
	}
	fmt.Print("Settings:\n")
	fmt.Printf(" - input file: %s\n", file)
	fmt.Printf(" - wikispeech url: %s\n", wikispeechURL)
	fmt.Printf(" - wikispeech language tag: %s\n", lang)
	fmt.Printf(" - max number of sentences: %s\n", nMaxS)
	fmt.Printf(" - save audio%s\n", saveAudioS)
	fmt.Printf("\n")

	if saveAudio {
		err := os.MkdirAll(audioDir, 0700)
		if err != nil {
			fmt.Printf("Failed to create audio dir : %v\n", err)
			os.Exit(1)
		}
	}

	mainStarted := time.Now()

	// Number of sentences sent concurrently to tts server
	// This is incremented each 'incEvery' sentences
	nSents := 1
	var sents []sent
	var n int

	sentsChan := make(chan sent)
	go readSents(file, sentsChan)

	for s := range sentsChan {
		if nMax > 0 && n >= nMax {
			fmt.Printf("Reached max no of sentences: %d\n", n)
			close(sentsChan)
			break
		}

		n++

		sents = append(sents, s)

		if len(sents) == nSents {
			tBefore := time.Now()
			zents := callSynthN(sents)
			for _, z := range zents {
				fmt.Printf("SENT: %d\t%s\nAUDIO LEN: %d\n", z.n, z.text, z.l)
				nChars := utf8.RuneCountInString(z.text)
				fmt.Printf("LEN DATA:\t#%d\t%d\t%d\t%f\n", z.n, nChars, z.l, float64(nChars)/float64(z.l))
				if z.err != nil {
					fmt.Printf("Failed call : %v\n", z.err)
					fmt.Printf("Number of sentences: %d\n", n)
					fmt.Printf("Concurrent sentences: %d\n", nSents)
					close(sentsChan)
					//break
					//os.Exit(1)

				}
			}
			tDuration := time.Since(tBefore).Seconds()
			fmt.Printf("SYNTH DUR: %fs\n", (tDuration / float64(nSents)))
			sents = nil // Empties the chache of sentences

			fmt.Println("------------")
		}
		if n%incEvery == 0 && nSents < maxConcurr {
			nSents++
		}
	}
	mainDuration := time.Since(mainStarted)
	fmt.Printf("MAIN LOOP TOOK %v\n", mainDuration.String())
	if saveAudio {
		fmt.Printf("AUDIO FILES SAVED TO FOLDER: %v\n", audioDir)
	}
}
