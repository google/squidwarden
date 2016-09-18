$(document).ready(function(){
    $("#about-websocket-server").text($("#websockets").val());
    var o = $("#about-websocket-client");
    o.text(!!window.WebSocket);
});
