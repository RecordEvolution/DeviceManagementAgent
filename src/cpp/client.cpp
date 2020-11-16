// ------------------------------------------------------------------------- //

#include <iostream>

#include "wampcc/kernel.h"
#include "wampcc/websocket_protocol.h"
#include "wampcc/wamp_session.h"
#include "wampcc/websocket_protocol.h"

#include <memory>
#include <iostream>

int main(int argc, char* argv[])
{
  std::cout<<"starting (C++)-WAMP client...\n";

  /* Create the wampcc kernel. */
  wampcc::kernel the_kernel;

  /* Create the TCP socket and attempt to connect. */
 // std::unique_ptr<wampcc::tcp_socket> socket(new wampcc::tcp_socket(&the_kernel));
 // socket->connect("127.0.0.1", 55555).wait_for(std::chrono::seconds(3));

//  if (!socket->is_connected())
//    throw std::runtime_error("connect failed");

  /* With the connected socket, create a wamp session & logon to the realm
   * * called 'default_realm'. */
//  auto session = wamp_session::create<websocket_protocol>(&the_kernel, std::move(socket));
  
//  session->hello("default_realm").wait_for(std::chrono::seconds(3));
  
//  if (!session->is_open())
//    throw std::runtime_error("realm logon failed");
  
  return 0;
}

// ------------------------------------------------------------------------- //
