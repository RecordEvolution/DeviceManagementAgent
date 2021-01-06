// ------------------------------------------------------------------------- //

#include <iostream>
#include "wampcc/wampcc.h"


int main(int argc, char* argv[])
{
  std::cout<<"starting (C++)-WAMP client...\n";

  /* Create the wampcc kernel, configured to support SSL. */
  wampcc::config conf;
  conf.ssl.enable = true;
  wampcc::kernel the_kernel(conf, wampcc::logger::console());

  /* Create the SSL socket, in connector mode. */
  wampcc::ssl_socket socket(&the_kernel);

  // try to connect
  // socket.connect("cb.reswarm.io",8080).wait_for(std::chrono::seconds(3));
  // if (!socket.is_connected())
  //   throw std::runtime_error("connect failed");

  /* Create the wampcc kernel. */
  // wampcc::kernel the_kernel;

  /* Create the TCP socket and attempt to connect. */
  // std::unique_ptr<wampcc::tcp_socket> socket(new wampcc::tcp_socket(&the_kernel));
  // socket->connect("cb.reswarm.io",8080).wait_for(std::chrono::seconds(3));
  //
  // if (!socket->is_connected())
  //   throw std::runtime_error("connect failed");

  /* With the connected socket, create a wamp session & logon to the realm
   * * called 'default_realm'. */
 // auto session = wampcc::wamp_session::create<wampcc::websocket_protocol>(&the_kernel, std::move(socket));
 //
 // session->hello("realm1").wait_for(std::chrono::seconds(3));
 //
 //  if (!session->is_open())
 //    throw std::runtime_error("realm logon failed");

  return 0;
}

// ------------------------------------------------------------------------- //
