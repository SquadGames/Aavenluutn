import React, {useState} from 'react'
import { useWallet } from 'use-wallet'

import { shortAddress } from '../utils'

export default function ConnectButton() {
  const [modalActive, setModalActive] = useState("")
  const wallet = useWallet()

  return (
    <div>
      {wallet.status === 'connected' ? (
        <a 
          className="button is-ghost is-medium is-size-5 is-vertical"
          onClick={() => wallet.reset()}
        >
          <span>Disconnect</span> 
          <span className="is-size-7">{shortAddress(wallet.account)}</span>
        </a>
      ) : (
        <a 
          className="button is-ghost is-medium is-size-5"
          onClick={() => setModalActive("is-active")}
        >
          Connect
        </a>
      )}
      <div className={`modal ${modalActive}`}>
        <div className="modal-background" onClick={()=>setModalActive("")} />
        <div className="modal-content has-background-white">
          <div className="box">
            <section className="section">
              <a 
                className="button is-ghost has-text-black"
                onClick={()=>{
                  wallet.connect("injected")
                  setModalActive("")
                }}
              >
                <img src="src/assets/images/metamask-fox.svg" alt="Metamask" width="80px"/>
                <h2 className="subtitle pl-5">Metamask</h2>
              </a>
            </section>
          </div>
        </div>
        <button className="modal-close is-large" onClick={()=>setModalActive("")} />
      </div>
    </div>
  )
}

// TODO Walletconnect