smtpd
=========================================================

A Lightweight High Performance SMTP written in Go, made for receiving 
large volumes of mail, parse and store in mongodb.

The purpose of this daemon is to grab the email, save it to the database
and disconnect as quickly as possible.

This server does not attempt to check for spam or do any sender 
verification. These steps should be performed by other programs.
The server does NOT send any email including bounces. This should
be performed by a separate program.

The most alluring aspect of Go are the Goroutines! It makes concurrent programming
easy, clean and fun! Go programs can also take advantage of all your machine's multiple 
cores without much effort that you would otherwise need with forking or managing your
event loop callbacks, etc. Golang solves the C10K problem in a very interesting way
 http://en.wikipedia.org/wiki/C10k_problem

Once compiled, Smtpd does not have an external dependencies (HTTP, SMTP are all built in).

Features
=========================================================

* ESMTP server implementing RFC5321
* Support for SMTP AUTH (RFC4954) and PIPELINING (RFC2920)
* Multipart MIME support
* UTF8 support for subject and message
* Web interface to view messages (plain text, HTML or source)
* Html sanitizer for html mail in web interface
* Real-time updates using websocket
* Download individual attachments
* MongoDB storage for message persistence
* Lightweight and portable
* No installation required

Development Status
=========================================================

SMTPD is currently production quality: it is being used for real work.


TODO
=========================================================

* POP3
* Rest API
* Inline resources in Web interface
* Per user/domain mailbox in web interface


Building from Source
=========================================================

You will need a functioning [Go installation][Golang] for this to work.

Grab the Smtpd source code and compile the daemon:

    ```go get -v github.com/gleez/smtpd```

Edit etc/smtpd.conf and tailor to your environment.  It should work on most
Unix and OS X machines as is.  Launch the daemon:

    ```$GOPATH/bin/smtpd -config=$GOPATH/src/github.com/gleez/smtpd/etc/smtpd.conf```

By default the SMTP server will be listening on localhost port 25000 and
the web interface will be available at [localhost:10025](http://localhost:10025/).

This will place smtpd in the background and continue running
	```/usr/bin/nohup /home/gleez/smtpd -config=/home/gleez/smtpd.conf -logfile=smtpd.log 2>&1 &```

You may also put another process to watch your smtpd process and re-start it
if something goes wrong.


Using Nginx as a proxy
=========================================================
Nginx can be used to proxy SMTP traffic for GoGuerrilla SMTPd

Why proxy SMTP?

 *	Terminate TLS connections: At present, only a partial implementation 
of TLS is provided. OpenSSL on the other hand, used in Nginx, has a complete 
implementation of SSL v2/v3 and TLS protocols.
 *	Could be used for load balancing and authentication in the future.

 1.	Compile nginx with --with-mail --with-mail_ssl_module
 2.	Configuration:

```
mail {
	#This is the URL to Smtpd's http service which tells Nginx where to proxy the traffic to
	auth_http 127.0.0.1:10025/;
					
	server {
		listen  15.29.8.163:25;
		protocol smtp;
		server_name  smtp.example.com;

		smtp_auth none;
		timeout 30000;
		smtp_capabilities "SIZE 15728640";

		# ssl default off. Leave off if starttls is on
		#ssl                   on;
		ssl_certificate        /etc/ssl/certs/ssl-cert-snakeoil.pem;
		ssl_certificate_key    /etc/ssl/private/ssl-cert-snakeoil.key;
		ssl_session_timeout    5m;

		ssl_protocols               SSLv2 SSLv3 TLSv1;
		ssl_ciphers                 HIGH:!aNULL:!MD5;
		ssl_prefer_server_ciphers   on;

		# TLS off unless client issues STARTTLS command
		starttls on;
		proxy    on;
	}
}
```

Credits
=========================================================
* https://github.com/flashmob/go-guerrilla
* https://github.com/jhillyerd/inbucket
* https://github.com/ian-kent/Go-MailHog
* https://github.com/briankassouf/incus
* https://github.com/microcosm-cc/bluemonday
* http://gorillatoolkit.org

Licence
=========================================================

Copyright ©‎ 2014, Gleez Technologies (http://www.gleeztech.com).

Released under MIT license, see [LICENSE](license) for details.