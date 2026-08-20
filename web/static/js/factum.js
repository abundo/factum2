// common factum javascript

function callSync(data) {
    console.log("data:", data);
    $.ajax({
        url: "/api/sync/" + data,
        method: "GET",
        data: {
            param1: "value1",
        },
        success: function (response) {
            // Do something with the response data
            console.log(response);
        },
    });
}

function prettyReplacer(key, value) {
    if (typeof value === "object") return `\n\t${JSON.stringify(value, null, 4)}\n`;
    return value;
}

function viewDevice(data) {
    console.log("data:", data);
    url = "/api/device";
    if (data != "") {
        url = url + "/name/" + data;
    }
    $.ajax({
        url: url,
        method: "GET",
        data: {
            param1: "value1",
        },
        success: function (response) {
            // Do something with the response data
            console.log(response);
            jsonString = JSON.stringify(response, (key, value) => {
                if (typeof value === "object") return JSON.stringify(value);
                return value;
            });
            // document.getElementById('result').innerHTML = jsonString
            // document.getElementById('result').innerHTML = JSON.stringify(response, prettyReplacer)
            document.getElementById("result").textContent = JSON.stringify(response, prettyReplacer);
        },
    });
}
