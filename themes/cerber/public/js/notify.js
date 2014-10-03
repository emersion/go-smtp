(function(){
	var unloaded = [];

	$(document).ready(function() {
		var socket = new Incus(getAbsolutePath(), '123456', '');

		socket.on('connect', function() {
			console.log('connected');
		});

		socket.on('NewMail', function(data) {
			$id = $('#NewMail')
			var i = parseInt($id.text()) || 0
			$id.text(i+1)
		});

		$('#AddToGreylist').on('click', function(e) {
			e.preventDefault()
			var href = $(this).attr("href")
			$.get(href, function(){})
		});
	})

	function getAbsolutePath() {
		return location.protocol+'//'+location.hostname+(location.port ? ':'+location.port: '');
	}
})()