# Golongpoll Examples
These instructions assume you are running them from within the ./examples directory.  Note the `-clientJs` flag sent to many of these examples. This is used to tell the http server where to find the static `js-client/client.js` file.

## Chatbot
This example demonstrates using both the javascript and go clients to create a dummy chatbot app. The webpage uses the javascript client while the go webserver spings up a goroutine that uses the go client subscribe to messages sent by the webpage and respond (publish) with response messages.

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
Todo: simple file persist, call out how kill and restart other examples, data lost.  Restart this, and data still there up to N seconds ago (configurable/based on client request params)


other todo:
* basic auth
* custom addon
* actual json not just str--use type swtich idom
* Both clients stop/quit behavior--include in one of the other examples?
* add publis() hooks to both go and js clients