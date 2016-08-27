var selected_group = 0;

$(document).ready(function() {
    //$("body").keypress(keypressHandler);
    $("#members-group-selection").change(function(e) {
	window.location.href = "/members/" + $(this).val();
    });
    //$("#button-update").click(update);
});
