package main

import (
	"compress/bzip2"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	Text      string     `xml:"corpus>qtext"`
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
	if strings.HasSuffix(corpusFile, ".xml.bz2") {
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

		s := sent{lang: "sv", text: l, n: n}
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

// Save audio file locally (default false)
var saveAudio bool

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

	urrl := "http://localhost:10000/?lang=" + s.lang + "&input=" + url.PathEscape(s.text)
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
	u, err := url.Parse(wsResp.Audio)
	if err != nil {
		res.err = fmt.Errorf("failed to write audio file : %v", err)
		resp <- res
		return //fmt.Errorf("failed to write audio file : %v", err)
	}
	if saveAudio {
		ioutil.WriteFile(filepath.Base(u.Path), aBody, 0755)
	}

	res.l = len(aBody)
	resp <- res

}

func main() {

	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "go run wsstrsstst.go <Text file> (one sentence per line)")
		fmt.Fprintln(os.Stderr, " OR")
		fmt.Fprintln(os.Stderr, "go run wsstrsstst.go <Språkbanken corpus file>\n - See https://spraakbanken.gu.se/eng/resources. The file can be in .bz2 or unzipped XML.")
		os.Exit(0)
	}

	f := os.Args[1]

	// Number of sentences sent cocurrently to tts server
	// This is incremented each 'incEvery' sentences
	nSents := 1
	var sents []sent
	var n int

	sentsChan := make(chan sent)
	go readSents(f, sentsChan)

	for s := range sentsChan {
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
}

// func main() {

// 	if len(os.Args) != 2 {
// 		fmt.Fprintln(os.Stderr, "<Språkbanken CORPUS FILE> (see https://spraakbanken.gu.se/eng/resources)\nThe file can be in .bz2 or unzipped XML")
// 		os.Exit(0)
// 	}

// 	f := os.Args[1]

// 	var xmlFile *os.File
// 	xmlFile, err := os.Open(f)
// 	if err != nil {
// 		fmt.Println("Error opening file:", err)
// 		return
// 	}
// 	defer xmlFile.Close()

// 	var decoder *xml.Decoder
// 	if strings.HasSuffix(f, ".bz2") {
// 		bz := bzip2.NewReader(xmlFile)
// 		decoder = xml.NewDecoder(bz)
// 	} else {
// 		decoder = xml.NewDecoder(xmlFile)
// 	}

// 	// Number of sentences sent cocurrently to tts server
// 	// This is incremented each incEvery sentences
// 	nSents := 1
// 	var sents []sent
// 	var words []string
// 	var n int
// 	for {

// 		// Read tokens from the XML document in a stream.
// 		t, _ := decoder.Token()
// 		if t == nil {
// 			break
// 		}
// 		// Inspect the type of the token just read.
// 		switch se := t.(type) {
// 		case xml.EndElement:
// 			//fmt.Println("END: " + se.Name.Local)
// 			if se.Name.Local == "sentence" {

// 				text := strings.Join(words, " ")
// 				words = nil
// 				n++

// 				s := sent{lang: "sv", text: text, n: n}
// 				sents = append(sents, s)
// 				if len(sents) == nSents {
// 					tBefore := time.Now()
// 					zents := callSynthN(sents)
// 					for _, z := range zents {
// 						fmt.Printf("SENT: %d\t%s\nAUDIO LEN: %d\n", z.n, z.text, z.l)
// 						nChars := utf8.RuneCountInString(z.text)
// 						fmt.Printf("LEN DATA:\t%d\t%d\t%d\t%f\n", z.n, nChars, z.l, float64(nChars)/float64(z.l))
// 						if z.err != nil {
// 							fmt.Printf("Failed call : %v\n", z.err)
// 							fmt.Printf("Number of sentences: %d\n", n)
// 							fmt.Printf("Concurrent sentences: %d\n", nSents)
// 							os.Exit(1)

// 						}
// 					}
// 					tDuration := time.Since(tBefore).Seconds()
// 					fmt.Printf("SYNTH DUR: %fs\n", (tDuration / float64(nSents)))
// 					sents = nil

// 					fmt.Println("------------")
// 				}
// 				if n%incEvery == 0 && nSents < maxConcurr {
// 					nSents++
// 				}
// 			}
// 		case xml.StartElement:
// 			elemName := se.Name.Local
// 			if !knownElems[elemName] {
// 				fmt.Printf("Unknown element: %s\n", elemName)
// 				os.Exit(1)
// 			}
// 			if elemName == "w" {
// 				w := W{}
// 				decoder.DecodeElement(&w, &se)
// 				words = append(words, w.W)
// 			}
// 		default: // ?
// 		}

// 	}

// }
