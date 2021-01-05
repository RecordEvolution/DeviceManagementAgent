

#include "parameters.hpp"

#include <autobahn/autobahn.hpp>
#include <boost/asio.hpp>
#include <iostream>
#include <memory>
#include <tuple>

class auth_wamp_session :
    public autobahn::wamp_session
{
public:
    boost::promise<autobahn::wamp_authenticate> challenge_future;
    std::string m_secret;

    auth_wamp_session(
            boost::asio::io_service& io,
            bool debug_enabled,
            const std::string& secret)
        : autobahn::wamp_session(io, debug_enabled)
        , m_secret(secret)
    {
    }

    boost::future<autobahn::wamp_authenticate> on_challenge(const autobahn::wamp_challenge& challenge)
    {
        std::cerr << "responding to auth challenge: " << challenge.challenge() << std::endl;
        std::string signature = compute_wcs(m_secret, challenge.challenge());
        challenge_future.set_value(autobahn::wamp_authenticate(signature));
        std::cerr << "signature: " << signature << std::endl;
        return challenge_future.get_future();
    }
};

int main(int argc, char** argv)
{
  std::cerr << "Boost: " << BOOST_VERSION << std::endl;

  try {
      auto parameters = get_parameters(argc, argv);

      boost::asio::io_service io;
      bool debug = true; //parameters->debug();

      parameters->set_rawsocket_endpoint("wss://cb.reswarm.io",8080);

      auto transport = std::make_shared<autobahn::wamp_tcp_transport>(
              io,
              parameters->rawsocket_endpoint(),
              debug);

      std::string secret = "44-3285";

      auto session = std::make_shared<auth_wamp_session>(io, debug, secret);

      transport->attach(std::static_pointer_cast<autobahn::wamp_transport_handler>(session));
  }
  catch (std::exception& e) {
      std::cerr << e.what() << std::endl;
      return 1;
  }

  return 0;
}
