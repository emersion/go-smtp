function Incus(url, UID, page) {
	this.MAXRETRIES   = 10;

	this.socketRetries = 0;
	this.pollRetries   = 0;
	this.attempts      = 1;

	this.url          = url;
	this.UID          = UID;
	this.page         = page;

	this.onMessageCbs = {};
	this.connectedCb  = false;

	this.socket          = null;
	this.poll            = null;
	this.connected       = false;
	this.socketConnected = false;

	this.connect();
}

Incus.prototype.longpoll = function(command) {
	if(this.socketConnected) { return; }

	if(this.poll != null) {
		this.poll.abort();
	}

	this.poll = new XMLHttpRequest();

	var data = {'user': this.UID};
	if(this.page) {
		data['page'] = this.page;
	}

	if(typeof command != 'undefined') {
		data['command'] = command;
	}
    
    var query_string = this.serialize(data);
    
    var self = this;
    this.poll.onreadystatechange = function() {
		if(self.poll.readyState == 4) {
			var response = {
				'data'   : self.poll.responseText,
				'status' : self.poll.status,
				'success': true
			};

			if(self.poll.status !== 0 && self.pollRetries < self.MAXRETRIES) {
			    self.longpoll();
			}

			if(response.status != 200 && response.status !== 0) {
				self.pollRetries++;
			}

			if(response.status == 200 && response.data !== "") {
				self.onMessage(response);
			}
		}
    }
    
	this.poll.open("GET", this.url+'/lp'+query_string, true);
	this.poll.timeout = 0;
	this.poll.send();

	this.connected = true;
    
	if(!this.connectedCb && "connect" in this.onMessageCbs) {
		this.connectedCb = true;
		this.onMessageCbs["connect"].call(null);
	}
}

Incus.prototype.connect = function() {
	this.longpoll();

	if("WebSocket" in window) {
		this.connectSocket();
	}
}

Incus.prototype.connectSocket = function() {
	var url = this.url.replace("https", "wss").replace("http", "ws");
	this.socket = new WebSocket(url+'/ws/');

	var self = this;
	this.socket.onopen    = function()  { self.authenticate() };
	this.socket.onmessage = function(e) { self.onMessage(e) };
	this.socket.onclose   = function()  { self.onClose() };
}

Incus.prototype.newCommand = function(command, message) {
	message['time'] = Math.round(new Date().getTime() / 1000);
	var obj = {
		"command": command,
		"message": message,
	};

	return JSON.stringify(obj);
}

Incus.prototype.authenticate = function() {
	this.socketConnected = true;
	this.poll.abort();

	var message = this.newCommand({'command': "authenticate", 'user': this.UID}, {});

	this.socket.send(message);

	if(this.page) {
		this.setPage(this.page);
	}

	if(!this.connectedCb && "connect" in this.onMessageCbs) {
		this.connectedCb = true;
		this.onMessageCbs["connect"].call(null);
	}

	this.connected = true;
}

Incus.prototype.on = function(name, func) {
	if(name == 'connect' && this.connected) {
		this.connectedCb = true;
		func();
	}

	this.onMessageCbs[name] = func;
}

Incus.prototype.onMessage = function(e) {
	if(e.data === "") {
		this.socketRetries = 0;
		this.attempts      = 1;
		return;
	}

	var msg = JSON.parse(e.data);

	if("event" in msg && msg.event in this.onMessageCbs) {
		if(typeof this.onMessageCbs[msg.event] == "function") {
			this.onMessageCbs[msg.event].call(null, msg.data);
		}
	}
}

Incus.prototype.onClose = function() {
	if(this.socketRetries > this.MAXRETRIES) {
		return
	}

	this.socketRetries++
	this.connected = false

	var time = this.generateInterval(this.attempts)
	,   self = this

	window.setTimeout(function() {
		console.log("Connection closed, retrying")
		// We've tried to reconnect so increment the attempts by 1
		self.attempts++

		self.connectSocket()
	}, time)
}

Incus.prototype.MessageUser = function(event, UID, data) {
	var command = {"command": "message", "user": UID}
	var message = {"event": event, "data": data}

	var msg = this.newCommand(command, message)
	return this.send(msg)
}

Incus.prototype.MessagePage = function(event, page, data) {
	var command = {"command": "message", "page": page}
	var message = {"event": event, "data": data}

	var msg = this.newCommand(command, message)
	return this.send(msg)
}

Incus.prototype.MessageAll = function(event, data) {
	var command = {"command": "message"}
	var message = {"event": event, "data": data}

	var msg = this.newCommand(command, message)
	return this.send(msg)
}

Incus.prototype.setPage = function(page) {
	this.page   = page;

	if(this.socketConnected) {
		var command = {'command': 'setpage', 'page': page};

		var msg = this.newCommand(command, {});
		return this.send(msg);
	}

	this.send();
}

Incus.prototype.serialize = function(obj) {
	var str = [];

	for(var p in obj){
		if(obj.hasOwnProperty(p)) {
			str.push(encodeURIComponent(p) + "=" + encodeURIComponent(obj[p]));
		}
	}
	return '?'+str.join("&");
}

Incus.prototype.send = function(command) {
	if(this.socketConnected) {
		this.socket.send(command);
	} else {
		this.longpoll(command);
	}
}

Incus.prototype.generateInterval = function(k) {
	var maxInterval = (Math.pow(2, k) - 1) * 1000;

	// If the generated interval is more than 30 seconds, truncate it down to 30 seconds.
	if (maxInterval > 30*1000) {
		maxInterval = 30*1000;
	}

	// If the generated interval is more than 120 seconds, truncate it down to 120 seconds.
	if (maxInterval > 120*1000) {
		maxInterval = 120*1000;
	}

	// If the generated interval is more than 5 mins, truncate it down to 5 mins.
	if (maxInterval > 300*1000) {
		maxInterval = 300*1000;
	}

	// generate the interval to a random number between 0 and the maxInterval determined from above
	return Math.round(Math.random() * maxInterval); 
}