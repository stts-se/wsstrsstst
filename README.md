# wsstrsstst
Simple script for calling the wikispeech_mockup server with sentences of a corpus file.


SAMPLE USAGE

* Start the Wikispeech server (and all its sub-servers)
* wget http://spraakbanken.gu.se/lb/resurser/meningsmangder/attasidor.xml.bz2
* go run wsstrsstst.go attasidor.xml.bz2

You can also read sentences from a simple text file (one sentence per line):

* go run wsstrsstst.go &lt;text file&gt;

The text file must have a .txt file extension.


Each 100 sentences, the server will be called with one more concurrent sentence, starting with one sentence at a time.
