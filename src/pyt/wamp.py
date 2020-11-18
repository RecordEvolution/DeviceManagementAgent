# --------------------------------------------------------------------------- #

from autobahn.twisted.component import Component, run
from autobahn.wamp import auth
import OpenSSL # python3 -m pip install pyopenssl service_identity
from twisted.internet.ssl import CertificateOptions

# read private key and certificate
with open('/home/mariof/Downloads/key.pem','r') as fin :
    key_pem = fin.read()
with open('/home/mariof/Downloads/cert.pem','r') as fin :
    cert_pem = fin.read()

# https://autobahn.readthedocs.io/en/latest/wamp/programming.html
demo = Component(
    transports=[{
        "type": "websocket",
        "url": "wss://cb.reswarm.io:8080",
        "endpoint": {
            "type": "tcp",
            "host": "cb.reswarm.io",
            "port": 8080,
            # https://twistedmatrix.com/documents/16.4.1/api/twisted.internet.ssl.CertificateOptions.html
            "tls": CertificateOptions(
                privateKey=OpenSSL.crypto.load_privatekey(OpenSSL.crypto.FILETYPE_PEM,key_pem),
                certificate=OpenSSL.crypto.load_certificate(OpenSSL.crypto.FILETYPE_PEM, cert_pem)
            )
        },
        "serializers": ['json']
    }],
    realm="realm1",
    authentication={
        u"wampcra": {
            u'authid': '{}-{}'.format(44, 3285),
            u'secret': "CZ3amCyKMxLsauC5+vGTZw=="
        }
    },
)

@demo.on_join
async def joined(session,details) :

    print(session)
    print(details)

# # 1. subscribe to a topic
# @demo.subscribe(u'realm1')
# def hello(msg):
#     print("Got hello: {}".format(msg))
#
# # 2. register a procedure for remote calling
# @demo.register(u're.mgmt.' + '3285' + '.docker_ps')
# def add2(x, y):
#     return x + y

# 3. after we've authenticated, run some code
# @demo.on_join
# async def joined(session, details):
#     # publish an event (won't go to "this" session by default)
#     await session.publish('realm1', 'Hello, world!')
#
#     # 4. call a remote procedure
#     result = await session.call('com.myapp.add2', 2, 3)
#     print("com.myapp.add2(2, 3) = {}".format(result))

if __name__ == "__main__" :

    print("starting WAMP client...")

    run([demo],log_level='debug')

# --------------------------------------------------------------------------- #
