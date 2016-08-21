$(document).ready(function() {
    $("#acl-selection").change(function(e) {
	window.location.href = "/acl/" + $(this).val();
    });
});
