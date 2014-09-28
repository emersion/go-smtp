var socket = new Incus(getAbsolutePath(), '123456', '');

socket.on('connect', function() {
	console.log('connected');
});

socket.on('NewMail', function(data) {
	$id = $('#NewMail')
	var i = parseInt($id.text()) || 0
	$id.text(i+1)
});

/*socket.on('Event', function(data) {
    alert(data);
});

socket.on('Event1', function (data) {
   console.log(data);
});

socket.on('Event2', function(data) {
    console.log('neat');
});

var data = {data: 'dummy-data'};

$('#button').on('click', function() {
    socket.MessageUser('Event', 'UID', data); 
    socket.MessageAll('Event1', data);
    socket.MessagePage('Event2', '/page/path', data);
});*/

function getAbsolutePath() {
	return location.protocol+'//'+location.hostname+(location.port ? ':'+location.port: '');
}

/*(function(){
	var unloaded = [];

	$(document).ready(function() {
		$('#htmlmsg img').each(function(idx, img) {
			unloaded.push(img.src)
			img._src = img.src;
			img.src = '';
		});

		console.log(unloaded);

		unloaded.each(function (image) {
			image.src = image._src;
		});
	})

})()*/