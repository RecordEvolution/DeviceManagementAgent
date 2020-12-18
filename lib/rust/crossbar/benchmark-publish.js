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

function randomInteger(min, max) {
  return Math.floor(Math.random() * (max - min + 1)) + min;
}

function sleep(ms) {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

async function delayed(session, amount, delay) {
  for (let i = 0; i < amount; i++) {
    session.publish(`reswarm.containers.helloworld`, [data || "Hello, world!"]);
    await sleep(delay);
  }
}

function burst(session, amount) {
  for (let i = 0; i < amount; i++) {
    session.publish(`reswarm.containers.helloworld`, [data || "Hello, world!"]);
  }
}

connection.onopen = async function (session) {
  while (true) {
    const randomAmount = randomInteger(1, 1000);
    burst(session, randomAmount);
    delayed(session, randomAmount, randomInteger(500, 800));
    await sleep(1000);
  }
};

connection.open();
