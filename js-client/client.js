var golongpoll = {
    newClient: function ({
            url,
            category,
            sinceTime=new Date().getTime(),
            lastId,
            onEvent = function (event) {},
            onFailure = function (errorMsg) { return true; },
            pollTimeoutSeconds=45,
            reattemptWaitSeconds=30,
            sucessWaitSeconds=0,
            basicAuthUsername="",
            basicAuthPassword="",
            loggingEnabled=false,
            extraRequestHeaders=[]
        }) {
            if (!url) {
                client.log("newClient() requires non-empty 'url' option.");
                return null;
            }

            if (!category || category.length < 1 || category.length > 1024) {
                client.log("newClient() requires 'category' option between 1-1024 characters long.");
                return null;
            }

            if (sinceTime <= 0) {
                client.log("newClient() requires 'sinceTime' option > 0.");
                return null;
            }

            if (pollTimeoutSeconds < 1) {
                client.log("newClient() requires 'pollTimeoutSeconds' option >= 1.");
                return null;
            }

            if (reattemptWaitSeconds < 1) {
                client.log("newClient() requires 'reattemptWaitSeconds' option >= 1.");
                return null;
            }

            if ((basicAuthUsername.length > 0 && basicAuthPassword.length == 0) || (basicAuthUsername.length == 0 && basicAuthPassword.length > 0)) {
                client.log("newClient() requires 'basicAuthUsername' and 'basicAuthPassword' to be both empty or nonempty, not mixed.");
                return null;
            }

            var client = {
                running: true,
                stop: function () {
                    client.log("golongpoll client signaling stop.");
                    this.running = false;
                },
                url: url,
                category: category,
                sinceTime: sinceTime,
                lastId: lastId,
                onEvent: onEvent,
                onFailure: onFailure,
                pollTimeoutSeconds: pollTimeoutSeconds,
                reattemptWaitSeconds: reattemptWaitSeconds,
                // amount of time after event, default to zero, no wait which is typical
                sucessWaitSeconds: sucessWaitSeconds,
                basicAuthUsername: basicAuthUsername || null,
                basicAuthPassword: basicAuthPassword || null,
                loggingEnabled: loggingEnabled,
                extraRequestHeaders: extraRequestHeaders,
                log: function (msg) {
                    if (this.loggingEnabled === true) {
                        if (typeof window.console == 'undefined') { return; };
                        console.log("golongpoll client[\"" + this.category + "\"]: " + msg);
                    }
                }
            };

            (function poll() {
                var params = {
                    category: client.category,
                    timeout: client.pollTimeoutSeconds,
                    since_time: client.sinceTime,
                    last_id: !client.lastId ? "" : client.lastId
                };
                var esc = encodeURIComponent;
                var query = Object.keys(params)
                    .map(function(k) {return esc(k) + '=' + esc(params[k]);})
                    .join('&');

                var pollUrl = client.url + "?" + query

                var xmlHttp = new XMLHttpRequest();

                xmlHttp.onreadystatechange = function () {
                    var req = xmlHttp;
                        if (req.readyState === 4) {
                            if (req.status === 200) {
                                var data = JSON.parse(req.responseText);

                                if (data && data.events && data.events.length > 0) {
                                    // got events, process them
                                    // NOTE: these events are in chronological order (oldest first)
                                    for (var i = 0; i < data.events.length; i++) {
                                        var event = data.events[i];
                                        // Update sinceTime to only request events that occurred after this one.
                                        client.sinceTime = event.timestamp;
                                        client.lastId = event.id; // used with sinceTime to pick up where we left off
                                        client.onEvent(event);
                                        if (!client.running) { // check if it's time to quit before continuing
                                            return;
                                        }
                                    }
                                    if (!client.running) { // check if it's time to quit before continuing
                                        return;
                                    }
                                    // success!  start next longpoll
                                    setTimeout(poll, client.sucessWaitSeconds * 1000);
                                    return;
                                } else if (data && data.timeout) {
                                    // no events within timeout window, start another longpoll:
                                    if (!client.running) { // check if it's time to quit before continuing
                                        return;
                                    }
                                    client.log("no events, requesting again.");
                                    setTimeout(poll, 0);
                                    return;
                                } else if (data && data.error) {

                                    client.log("got error response: " + data.error);
                                    if (!client.running) { // check if it's time to quit before continuing
                                        return;
                                    }
                                    if (!client.onFailure(data.error)) {
                                        client.log("stopping due to onFailure returning false");
                                        return;
                                    }
                                    setTimeout(poll, client.reattemptWaitSeconds * 1000);
                                    return;
                                } else {
                                    client.log("got unexpected response: " + data);
                                    if (!client.running) { // check if it's time to quit before continuing
                                        return;
                                    }
                                    if (!client.onFailure("client got unexpected response: " + data)) {
                                        client.log("stopping due to onFailure returning false.");
                                        return;
                                    }
                                    setTimeout(poll, client.reattemptWaitSeconds * 1000);
                                }
                            } else {
                                client.log("request FAILED, response status: " + req.status);
                                if (!client.running) { // check if it's time to quit before continuing
                                    return;
                                }
                                if (!client.onFailure("request FAILED, response status: " + req.status)) {
                                    client.log("stopping due to onFailure returning false.");
                                    return;
                                }
                                setTimeout(poll, client.reattemptWaitSeconds * 1000)
                                return;
                            }
                        }
                };
                // NOTE: includes optional user/password for basic auth
                xmlHttp.open("GET", pollUrl, true, client.basicAuthUsername, client.basicAuthPassword); // true for asynchronous

                // Add any optional request headers
                for (var i = 0; i < client.extraRequestHeaders.length; i++) {
                    xmlHttp.setRequestHeader(client.extraRequestHeaders[i].key, client.extraRequestHeaders[i].value);
                }

                xmlHttp.send(null);
            })();

        return client;
    }
};
