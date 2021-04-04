# Golongpoll Examples
These instructions assume you are running them from within the ./examples directory.  Note the `-clientJs` flag sent to many of these examples. This is used to tell the http server where to find the static `js-client/client.js` file.

## Chatbot
This example demonstrates using both the javascript and go clients to create a dummy chatbot app. The webpage uses the javascript client while the go webserver spins up a goroutine that uses the go client to subscribe to messages sent by the webpage and respond (publish) with response messages.

To Build:
```
go build -o chatbot_example chatbot/chatbot.go
```
Note that we rename the output `chatbot_example` since there's already a folder called `chatbot`

To Run:
```
./chatbot_example -clientJs ../js-client/client.js
```
Then visit: `http://127.0.0.1:8101/chatbot`

## FilePersist
In this example, you can publish events via webpage and they'll be saved to file via the `FilePersistorAddOn`.  Test this out by publishing some events, then kill the program, restart and observe the events still show up.

To Build:
```
go build -o filepersist_example filepersist/filepersist.go
```

To Run:
```
./filepersist_example -clientJs ../js-client/client.js -persistTo ./filepersist_example.data
```
Then visit: `http://127.0.0.1:8102/filepersist`




2) client (js go) header(s) option plural

3) custom addon example

4) auth example -- basic via opts or basic via header

TODO: client and js client optional header(s) to send when making requests
    >> for any extra auth/stuff
    >> test by using basic auth this way instead

then, remaining examples:
* add publish() hooks to both go and js clients

* Both clients stop/quit behavior--include in one of the other examples?
    >> or just test and not include?

all of these as chatbotplus:
* basic auth
* actual json not just str--use type swtich idom
* file persist

lastly: make sure all examples build via readme steps, can access url in readme and stdout!
