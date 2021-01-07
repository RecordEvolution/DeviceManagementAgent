#
# crossbar WAMP router:
# $ docker pull crossbario/crossbar
# $ docker run -it --rm -p 8080:8080 crossbario/crossbar
#
from autobahn.twisted.component import Component
from autobahn.twisted.component import run
from twisted.internet.defer import inlineCallbacks
import time
import math

comp = Component(
    # transports=u"ws://localhost:8080/ws",
    # realm=u"realm1",
    transports=u"ws://localhost:55555",
    realm=u"default_realm",
)

@comp.on_join
def joined(session, details):

    print("session ready")

    callinghello(session,details)
    registering(session,details)

    # testTimeSeries(session,details)
    # setup test time series
    for i in range(0,15) :
        callingadd(session,details,i)

    # session.close()

# @inlineCallbacks
# def testTimeSeries(session, details):
#
#     # setup test time series
#     for i in range(0,15) :
#         callingadd(session,details,i)

@inlineCallbacks
def registering(session, details):

    def add2(x, y):
        print("add2 called...")
        return x + y

    try:
        yield session.register(add2, u'com.myapp.add2')
        print("procedure registered")
    except Exception as e:
        print("could not register procedure: {0}".format(e))

@inlineCallbacks
def callinghello(session, details):

    try:
        res = yield session.call(u'greeting')
        print("call result: {}".format(res))
    except Exception as e:
        print("call error: {0}".format(e))


@inlineCallbacks
def callingadd(session, details, num):

    try:
        # res = yield session.call(u'com.myapp.add2', 2, 3)
        res = yield session.call(u'math.service.add', 2, 3, num)
        print("call result: {}".format(res))
    except Exception as e:
        print("call error: {0}".format(e))


if __name__ == "__main__":
    run([comp])
