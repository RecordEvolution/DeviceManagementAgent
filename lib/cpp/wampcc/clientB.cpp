// ------------------------------------------------------------------------- //

#include <iostream>
#include "wampcc/wampcc.h"


int main(int argc, char* argv[])
{
  std::cout<<"starting another (C++)-WAMP client...\n";

  /* Create the wampcc kernel, configured to support SSL. */
  wampcc::config conf;
  conf.ssl.enable = true;
  wampcc::kernel the_kernel(conf, wampcc::logger::console());

  /* Create the TCP socket and attempt to connect. */
  std::unique_ptr<wampcc::tcp_socket> socket(new wampcc::tcp_socket(&the_kernel));
  socket->connect("127.0.0.1", 55555).wait_for(std::chrono::seconds(3));

  if (!socket->is_connected())
    throw std::runtime_error("connect failed");

  /* With the connected socket, create a wamp session & logon to the realm
   * called 'default_realm'. */
  auto session = wampcc::wamp_session::create<wampcc::websocket_protocol>(&the_kernel,
                                                                          std::move(socket));

  session->hello("default_realm").wait_for(std::chrono::seconds(3));

  if (!session->is_open())
    throw std::runtime_error("realm logon failed");

  /* Call a remote procedure. */
  session->call("greeting", {}, {},
    [](wampcc::wamp_session&, wampcc::result_info result) {
      if (result)
        std::cout << "got result: " << result.args.args_list[0] << std::endl;
      else
        std::cout<<"greeting call failed\n";
    }
  );

  session->call("math.service.add", {}, {{17, 23}, {}},
    [](wampcc::wamp_session&, wampcc::result_info result) {
      if (result)
        std::cout << "got result: " << result.args.args_list[0] << std::endl;
      else
        std::cerr<<"math.service.add call failed"<<std::endl;
    }
  );

  session->closed_future().wait_for(std::chrono::seconds(10));
  session->close().wait();

  std::cout<<"finishing another (C++)-WAMP client...waiting idle..."<<std::endl;

  std::promise<void> forever;
  forever.get_future().wait();

  return 0;
}

// ------------------------------------------------------------------------- //
