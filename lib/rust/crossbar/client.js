try {
  // for Node.js
  var autobahn = require("autobahn");
} catch (e) {
  // for browsers (where AutobahnJS is available globally)
}

const [topic_endpoint, data] = process.argv.slice(2);

var connection = new autobahn.Connection({
  url: "ws://localhost:8080/ws",
  realm: "realm1",
});

connection.onopen = function (session) {
  console.log("Publishing to", `reswarm.containers.${topic_endpoint}`);
  session.publish(`reswarm.containers.${topic_endpoint}`, [data || "Hello, world!"]);
  connection.close()
};

connection.open();
