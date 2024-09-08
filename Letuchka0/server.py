#!/usr/bin/python3

import asyncio 
import websockets

async def hello(websocket, path):
    name = await websocket.recv()
    print("client say: ")

    while(True):
        greeting = input("enter answer to client: ")
        await websocket.send(greeting)
        print(str(greeting))

start_server = websockets.serve(hello, "185.104.251.226", 9742)
asyncio.get_event_loop().run_until_complete(start_server)
asyncio.get_event_loop().run_forever()
