# wsstrsstst
Simple script for calling the wikispeech_mockup server with sentences of a corpus file.


SAMPLE USAGE

* Start the Wikispeech server (and all its sub-servers)
* `wget http://spraakbanken.gu.se/lb/resurser/meningsmangder/attasidor.xml.bz2`
* `go run wsstrsstst.go attasidor.xml.bz2`

You can also read sentences from a simple text file (one sentence per line):

`$ go run wsstrsstst.go <text file>`

The text file must have a .txt file extension.


You can get a full list of options by running   
`$ go run wsstrsstst.go`

     go run <flags> wsstrsstst.go <Text file> (one sentence per line)
      OR
     go run <flags> wsstrsstst.go <Språkbanken corpus file>
      - See https://spraakbanken.gu.se/eng/resources. The file can be in .bz2 or unzipped XML.
     
     Optional flags:
       -a	save audio files to disk (default false)
       -l string
         	wikispeech language tag (default "sv")
       -n int
         	max number of sentences to synthesize (default no limit)
       -u string
         	wikispeech url (default "http://localhost:10000")



Each 100 sentences, the server will be called with one more concurrent sentence, starting with one sentence at a time.
