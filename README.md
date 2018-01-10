# wsstrsstst
Simple script for calling the wikispeech_mockup server with sentences of a corpus file.


SAMPLE USAGE:

o Start the Wikispeech server (and all its sub-servers)
o wget http://spraakbanken.gu.se/lb/resurser/meningsmangder/attasidor.xml.bz2
o go run wsstrsstst.go attasidor.xml.bz2

Each 100 sentences, the server will be called with one more concurrent sentence, starting with one sentence at a time.
